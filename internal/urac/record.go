// Package urac reads the logged-in user as the Node.js gateway does: soajs.urac.driver getRecord
// for the record, and mw/mt/urac.js for what the gateway does with it (docs/data-schema/urac.md).
//
// Users and groups are read in one database, coreDB[masterDB], where masterDB is the custom
// registry urac value.masterDB. A missing masterDB, or one that is not a core database of the
// environment, is a configuration error.
package urac

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/dlclark/regexp2/v2"
	"github.com/soajs/soajs.controller.go/internal/dataschema"
	"github.com/soajs/soajs.controller.go/internal/dbconfig"
	"github.com/soajs/soajs.controller.go/pkg/types"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	drv "go.mongodb.org/mongo-driver/mongo"
	"go.uber.org/zap"
)

// Error is a soajs.urac.driver error, with its code and message (config.js).
type Error struct {
	Code int
	Msg  string
}

func (e *Error) Error() string { return e.Msg }

var errorMessages = map[int]string{
	400: "Model error",
	403: "Unable to log in. Credential error or mismatch",
	404: "Unable to log in. Credential error or mismatch",
	405: "Unable to log in the user. User has not activated this account",
	406: "Unable to log in the user. User is inactive",
	407: "User does not have access to the tenant",
}

func driverError(code int, detail string) *Error {
	msg := errorMessages[code]
	if detail != "" {
		msg += " - " + detail
	}
	return &Error{Code: code, Msg: msg}
}

// emailPattern is the soajs.urac.driver pattern (index.js) that decides if a username is an
// email. It uses lookahead, so it runs in ECMAScript mode; its nested quantifiers are bounded by a
// match timeout.
var emailPattern = func() *regexp2.Regexp {
	re := regexp2.MustCompile(`^(?:[\w!#$%&'*+\-\/=?^`+"`"+`{|}~]+\.)*[\w!#$%&'*+\-\/=?^`+"`"+`{|}~]+@(?:(?:(?:[a-zA-Z0-9](?:[a-zA-Z0-9\-](?!\.)){0,61}[a-zA-Z0-9]?\.)+[a-zA-Z0-9](?:[a-zA-Z0-9\-](?!$)){0,61}[a-zA-Z0-9]?)|(?:\[(?:(?:[01]?\d{1,2}|2[0-4]\d|25[0-5])\.){3}(?:[01]?\d{1,2}|2[0-4]\d|25[0-5])]))$`, regexp2.ECMAScript)
	re.MatchTimeout = 100 * time.Millisecond
	return re
}()

// IsEmail reports whether a username is looked up by email.
func IsEmail(username string) (bool, error) {
	return emailPattern.MatchString(username)
}

// Store reads users and groups.
type Store interface {
	// FindUser returns the user matching filter, nil when there is none.
	FindUser(ctx context.Context, filter bson.M) (map[string]interface{}, error)
	// FindGroups returns the groups matching filter.
	FindGroups(ctx context.Context, filter bson.M) ([]map[string]interface{}, error)
}

// Databases opens a database from its configuration.
type Databases func(db *dbconfig.DB) (*drv.Database, error)

// Driver reads user records.
type Driver struct {
	databases Databases
	logger    *zap.Logger
	// store, when set, replaces the masterDB store (tests).
	store Store
}

// NewDriver returns a driver reading coreDB[masterDB] through databases.
func NewDriver(databases Databases, logger *zap.Logger) *Driver {
	return &Driver{databases: databases, logger: logger}
}

// NewDriverWithStore returns a driver reading store.
func NewDriverWithStore(store Store, logger *zap.Logger) *Driver {
	return &Driver{store: store, logger: logger}
}

// MasterDB is the configuration of the urac database: coreDB[masterDB].
func MasterDB(reg *types.RegistryData) (*dbconfig.DB, error) {
	masterDB, _ := uracValue(reg)["masterDB"].(string)
	if masterDB == "" {
		return nil, errors.New("configuration error: the custom registry urac masterDB is missing")
	}
	db := reg.CoreDB[masterDB]
	if db == nil {
		return nil, fmt.Errorf("configuration error: urac masterDB [%s] is not a core database of the environment", masterDB)
	}
	return db, nil
}

func uracValue(reg *types.RegistryData) map[string]interface{} {
	if reg == nil {
		return nil
	}
	entry, _ := reg.CustomRegistry["urac"].(map[string]interface{})
	value, _ := entry["value"].(map[string]interface{})
	return value
}

func (d *Driver) storeFor(reg *types.RegistryData) (Store, error) {
	if d.store != nil {
		return d.store, nil
	}
	cfg, err := MasterDB(reg)
	if err != nil {
		return nil, err
	}
	db, err := d.databases(cfg)
	if err != nil {
		return nil, err
	}
	return &mongoStore{db: db, logger: d.logger}, nil
}

// Tenant is the tenant of the request, as the urac driver sees it.
type Tenant struct {
	ID      string
	Code    string
	Roaming *Roaming
}

// Roaming is set when a token from a sensitive environment is used in another one (uracCheck).
type Roaming struct {
	TID  string
	ID   string
	Code string
	User map[string]interface{}
	// TenantMetaDB is the tenantMetaDB of the token's environment.
	TenantMetaDB interface{}
}

// GetRecord reads the user as soajs.urac.driver getRecord does: by email when the username looks
// like one, by username, else by _id; only an active user found by name passes. The user must
// have access to the tenant: its home tenant, an invitation (config.allowedTenants), roaming, or
// the custom registry urac autoRoaming. Under another tenant the user carries that tenant and
// only the groups it grants there. The groups' allowed packages are merged into
// groupsConfig.allowedPackages.
func (d *Driver) GetRecord(ctx context.Context, reg *types.RegistryData, tenant Tenant, username, id string) (map[string]interface{}, error) {
	store, err := d.storeFor(reg)
	if err != nil {
		return nil, driverError(400, err.Error())
	}
	var record map[string]interface{}
	switch {
	case username != "":
		email, err := IsEmail(username)
		if err != nil {
			return nil, driverError(400, err.Error())
		}
		filter := bson.M{"username": username}
		if email {
			filter = bson.M{"email": username}
		}
		record, err = store.FindUser(ctx, filter)
		if err != nil {
			return nil, driverError(400, err.Error())
		}
		if record != nil {
			status, _ := record["status"].(string)
			switch {
			case status == "active":
			case containsPending(status):
				d.logger.Error("user status is pending", zap.String("_id", str(record["_id"])), zap.String("status", status))
				return nil, driverError(405, "")
			case status == "inactive":
				d.logger.Error("user status is inactive", zap.String("_id", str(record["_id"])))
				return nil, driverError(406, "")
			default:
				record = nil
			}
		}
	case id != "":
		oid, err := primitive.ObjectIDFromHex(id)
		if err != nil {
			return nil, driverError(404, "")
		}
		record, err = store.FindUser(ctx, bson.M{"_id": oid})
		if err != nil {
			return nil, driverError(400, err.Error())
		}
	}
	if record == nil {
		return nil, driverError(403, "")
	}
	delete(record, "password")

	autoRoaming, _ := uracValue(reg)["autoRoaming"].(map[string]interface{})
	access := checkUserTenantAccess(record, tenant, autoRoaming)
	if access == nil {
		d.logger.Error("user has no access to the tenant", zap.String("_id", str(record["_id"])), zap.String("tenant", tenant.ID))
		return nil, driverError(407, "")
	}
	if access.other {
		record["tenant"] = access.tenant
		if groups, ok := access.groups.([]interface{}); ok {
			record["groups"] = groups
		} else {
			record["groups"] = []interface{}{}
		}
	}
	if groups, ok := access.groups.([]interface{}); ok && len(groups) > 0 {
		record["groups"] = groups
		record["tenant"] = access.tenant
		found, err := store.FindGroups(ctx, bson.M{"tenant.id": tenant.ID, "code": bson.M{"$in": groups}})
		if err != nil {
			return nil, driverError(400, err.Error())
		}
		if len(found) > 0 {
			record["groupsConfig"] = found
		}
	}
	assureConfig(record)
	return record, nil
}

func containsPending(status string) bool {
	for i := 0; i+len("pending") <= len(status); i++ {
		if status[i:i+len("pending")] == "pending" {
			return true
		}
	}
	return false
}

type tenantAccess struct {
	groups interface{}
	tenant interface{}
	// other is set when the access is through another tenant than the user's own
	other bool
}

// checkUserTenantAccess is soajs.urac.driver lib/common.js checkUserTenantAccess.
func checkUserTenantAccess(record map[string]interface{}, tenant Tenant, autoRoaming map[string]interface{}) *tenantAccess {
	home, _ := record["tenant"].(map[string]interface{})
	if home == nil || tenant.ID == "" {
		return nil
	}
	homeID, _ := home["id"].(string)
	if homeID == tenant.ID {
		return &tenantAccess{groups: record["groups"], tenant: home}
	}
	config, _ := record["config"].(map[string]interface{})
	if allowed, ok := config["allowedTenants"].([]interface{}); ok {
		for _, a := range allowed {
			entry, _ := a.(map[string]interface{})
			t, _ := entry["tenant"].(map[string]interface{})
			if t != nil && t["id"] == tenant.ID {
				return &tenantAccess{groups: entry["groups"], tenant: t, other: true}
			}
		}
	}
	if tenant.Roaming != nil && homeID == tenant.Roaming.TID {
		return &tenantAccess{groups: record["groups"], tenant: home}
	}
	if byTenant, ok := autoRoaming[tenant.ID].(map[string]interface{}); ok {
		if rule, ok := byTenant[homeID].(map[string]interface{}); ok {
			return &tenantAccess{
				groups: rule["groups"],
				tenant: map[string]interface{}{"id": tenant.ID, "code": tenant.Code},
				other:  true,
			}
		}
	}
	return nil
}

// assureConfig merges the allowed packages of the user's groups, by product
// (soajs.urac.driver lib/user.js assureConfig).
func assureConfig(record map[string]interface{}) {
	if _, ok := record["config"].(map[string]interface{}); !ok {
		record["config"] = map[string]interface{}{}
	}
	groups, _ := record["groupsConfig"].([]map[string]interface{})
	if len(groups) == 0 {
		return
	}
	merged := map[string]interface{}{}
	for _, g := range groups {
		config, _ := g["config"].(map[string]interface{})
		packages, _ := config["allowedPackages"].(map[string]interface{})
		for product, value := range packages {
			// as Node.js: the first truthy value as it is, then [...new Set([...current, ...value])],
			// where spreading a string gives its characters
			current, has := merged[product]
			if !has || !dataschema.Truthy(current) {
				merged[product] = value
				continue
			}
			merged[product] = union(spread(current), spread(value))
		}
	}
	record["groupsConfig"] = map[string]interface{}{"allowedPackages": merged}
}

// spread is [...v]: a list's items, a string's characters (code points).
func spread(v interface{}) []interface{} {
	switch t := v.(type) {
	case []interface{}:
		return t
	case string:
		out := make([]interface{}, 0, len(t))
		for _, r := range t {
			out = append(out, string(r))
		}
		return out
	}
	return nil
}

// union is [...new Set([...a, ...b])]: SameValueZero for strings, numbers and booleans; objects and
// lists are never the same.
func union(a, b []interface{}) []interface{} {
	out := []interface{}{}
	seen := map[interface{}]bool{}
	for _, list := range [][]interface{}{a, b} {
		for _, v := range list {
			switch v.(type) {
			case map[string]interface{}, []interface{}:
				out = append(out, v)
				continue
			}
			if !seen[v] {
				seen[v] = true
				out = append(out, v)
			}
		}
	}
	return out
}

func str(v interface{}) string {
	s, _ := v.(string)
	return s
}

// mongoStore reads users and groups in the urac database; a document that does not match the data
// schema is logged by _id and skipped.
type mongoStore struct {
	db     *drv.Database
	logger *zap.Logger
}

func (s *mongoStore) FindUser(ctx context.Context, filter bson.M) (map[string]interface{}, error) {
	raw, err := s.db.Collection("users").FindOne(ctx, filter).Raw()
	if err == drv.ErrNoDocuments {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return s.read("users", dataschema.Users, raw)
}

func (s *mongoStore) FindGroups(ctx context.Context, filter bson.M) ([]map[string]interface{}, error) {
	cur, err := s.db.Collection("groups").Find(ctx, filter)
	if err != nil {
		return nil, err
	}
	defer cur.Close(ctx)
	var out []map[string]interface{}
	for cur.Next(ctx) {
		// a group that cannot be read gives no packages, nor does an allowedPackages entry that
		// cannot be read: as Node.js, the key's package ACL applies when nothing else gives one
		g, err := s.read("groups", dataschema.Groups, cur.Current)
		if err != nil {
			return nil, err
		}
		if g != nil {
			out = append(out, g)
		}
	}
	return out, cur.Err()
}

// read reads a users or groups document through its policy and reports what it found
// (/schemaProblems). It returns nil for a document that cannot be read. What was left out of a
// user's roles (its groups, a group, a group's allowedPackages entry) is reported as falling back
// to the key's package: the gateway follows Node.js there (docs/data-schema/imperfect-data.md).
func (s *mongoStore) read(coll string, policy dataschema.Policy, raw bson.Raw) (map[string]interface{}, error) {
	schemas, err := dataschema.Embedded()
	if err != nil {
		return nil, err
	}
	schema := schemas[coll]
	if schema == nil {
		return nil, fmt.Errorf("no data schema for %s", coll)
	}
	var doc map[string]interface{}
	res := schema.Read(raw, policy, &doc)
	for i, f := range res.Findings {
		piece := f.Piece
		if piece == "" {
			piece = f.Path
		}
		switch {
		case coll == "groups" && f.Action == dataschema.SkippedDocument,
			coll == "groups" && f.Action == dataschema.DroppedPiece && strings.HasPrefix(piece, "/config/allowedPackages/"),
			coll == "users" && f.Action == dataschema.DroppedPiece && (piece == "/groups" || strings.HasPrefix(piece, "/groups/")):
			res.Findings[i].Action = dataschema.FellBackToKeyPackage
		}
	}
	dataschema.Problems.ReportResult(s.logger, res)
	if !res.OK {
		return nil, nil
	}
	return doc, nil
}
