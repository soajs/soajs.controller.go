package provision

import (
	"context"
	"os"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	drv "go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.uber.org/zap"

	"github.com/soajs/soajs.controller.go/internal/dataschema"
	"github.com/soajs/soajs.controller.go/pkg/types"
)

// an access token with its expires (and, when given, its env and user) replaced (test values)
func tokenDoc(token, expires, env, user string) string {
	if env == "" {
		env = "dev"
	}
	if user == "" {
		user = `{"_id":{"$oid":"6a0e000000000000000000b7"},"id":"6a0e000000000000000000b7","username":"u","loginMode":"urac","deviceId":null,
		"tenant":{"id":"6a0e00000000000000000001","code":"CONF"}}`
	}
	doc := `{"_id":{"$oid":"6a0e0000000000000000` + token[len(token)-4:] + `"},"token":"` + token + `","type":"accessToken","clientId":"c","env":"` + env + `",`
	if expires != "" {
		doc += `"expires":` + expires + `,`
	}
	return doc + `"user":` + user + `}`
}

func rawDoc(t *testing.T, doc string) bson.Raw {
	t.Helper()
	var d bson.D
	if err := bson.UnmarshalExtJSON([]byte(doc), false, &d); err != nil {
		t.Fatal(err)
	}
	raw, err := bson.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// A token refused only because its expires is not a date reads again with expires at the epoch;
// a token refused for anything else does not.
func TestReadWithEpochExpires(t *testing.T) {
	schema, err := schemaFor("oauth_token")
	if err != nil {
		t.Fatal(err)
	}
	userWithoutID := `{"_id":{"$oid":"6a0e000000000000000000b7"},"username":"u","loginMode":"urac","deviceId":null,"tenant":{"id":"6a0e00000000000000000001","code":"CONF"}}`
	cases := []struct {
		name, doc string
		expired   bool
	}{
		{"expires null", tokenDoc("c0f0000000000000000000000000000000000001", `null`, "", ""), true},
		{"expires a string", tokenDoc("c0f0000000000000000000000000000000000002", `"2099-01-01T00:00:00Z"`, "", ""), true},
		{"expires a number", tokenDoc("c0f0000000000000000000000000000000000003", `4102444800000`, "", ""), true},
		{"no expires", tokenDoc("c0f0000000000000000000000000000000000004", ``, "", ""), true},
		{"expires null and no user id", tokenDoc("c0f0000000000000000000000000000000000005", `null`, "", userWithoutID), false},
		{"no user id", tokenDoc("c0f0000000000000000000000000000000000006", `{"$date":"2099-01-01T00:00:00Z"}`, "", userWithoutID), false},
	}
	for _, c := range cases {
		raw := rawDoc(t, c.doc)
		out := &types.AccessToken{}
		first := schema.Read(raw, dataschema.OAuthTokens, out)
		if first.OK {
			t.Fatalf("%s: read without the re-read", c.name)
		}
		got := readWithEpochExpires(schema, raw, first, out)
		if got != c.expired {
			t.Errorf("%s: read again = %v, want %v (findings %+v)", c.name, got, c.expired, first.Findings)
			continue
		}
		if got && (!out.Expires.Equal(time.Unix(0, 0)) || !out.Expires.Before(time.Now()) || out.Token == "" || out.User["id"] == nil) {
			t.Errorf("%s: token read again = %+v, want it whole with expires at the epoch", c.name, out)
		}
	}
}

// GetAccessToken gives a token whose expires is not a date as expired (expires at the epoch), so
// the gateway answers "has expired"; the environment rule and every other refusal are unchanged.
func TestGetAccessTokenExpiresNotADate(t *testing.T) {
	uri := os.Getenv("MONGO_URI")
	if uri == "" {
		uri = "mongodb://127.0.0.1:27017"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client, err := drv.Connect(ctx, options.Client().ApplyURI(uri).SetServerSelectionTimeout(2*time.Second))
	if err == nil {
		err = client.Ping(ctx, nil)
	}
	if err != nil {
		t.Skip("no local MongoDB: " + err.Error())
	}
	defer client.Disconnect(context.Background())
	db := client.Database("accesstoken_provision_test")
	_ = db.Drop(ctx)
	defer db.Drop(context.Background())
	userWithoutID := `{"_id":{"$oid":"6a0e000000000000000000b7"},"username":"u","loginMode":"urac","deviceId":null,"tenant":{"id":"6a0e00000000000000000001","code":"CONF"}}`
	docs := map[string]string{
		"c0f0000000000000000000000000000000000011": tokenDoc("c0f0000000000000000000000000000000000011", `null`, "", ""),
		"c0f0000000000000000000000000000000000012": tokenDoc("c0f0000000000000000000000000000000000012", `"2099-01-01T00:00:00Z"`, "", ""),
		"c0f0000000000000000000000000000000000013": tokenDoc("c0f0000000000000000000000000000000000013", ``, "", ""),
		"c0f0000000000000000000000000000000000014": tokenDoc("c0f0000000000000000000000000000000000014", `null`, "stg", ""),
		"c0f0000000000000000000000000000000000015": tokenDoc("c0f0000000000000000000000000000000000015", `null`, "", userWithoutID),
		"c0f0000000000000000000000000000000000016": tokenDoc("c0f0000000000000000000000000000000000016", `{"$date":"2099-01-01T00:00:00Z"}`, "", ""),
	}
	for _, doc := range docs {
		if _, err := db.Collection("oauth_token").InsertOne(ctx, rawDoc(t, doc)); err != nil {
			t.Fatal(err)
		}
	}
	m := &mongoOAuthModel{db: db, env: "dev", logger: zap.NewNop()}
	for token, want := range map[string]string{
		"c0f0000000000000000000000000000000000011": "expired",
		"c0f0000000000000000000000000000000000012": "expired",
		"c0f0000000000000000000000000000000000013": "expired",
		"c0f0000000000000000000000000000000000014": "none", // another, not sensitive, environment
		"c0f0000000000000000000000000000000000015": "none", // refused for its user as well
		"c0f0000000000000000000000000000000000016": "valid",
	} {
		record, err := m.GetAccessToken(token)
		if err != nil {
			t.Fatal(err)
		}
		got := "valid"
		switch {
		case record == nil:
			got = "none"
		case record.Expires.Before(time.Now()):
			got = "expired"
		}
		if got != want {
			t.Errorf("%s: %s, want %s", token, got, want)
		}
	}
}
