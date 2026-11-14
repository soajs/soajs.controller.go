package types

import "regexp"

// FinalACL is the ACL of the requested service version for the request's method, as the Node.js
// keyACL middleware builds it (finalAcl): the version's access and apisPermission, and its APIs
// for the method, those with path parameters ("/:") as regular expressions.
type FinalACL struct {
	// Access is true when the service version needs a logged-in user.
	Access bool
	// APIsPermission is "restricted" when only the listed APIs are allowed.
	APIsPermission string
	APIs           map[string]*ACLAPI
	APIsRegExp     []*ACLAPI
}

// ACLAPI is one API of a FinalACL.
type ACLAPI struct {
	// Access is true when the API needs a logged-in user.
	Access bool
	Group  string
	Route  string
	RegExp *regexp.Regexp
}

// API finds the ACL of an API path: an exact route, else the last route whose expression matches,
// as the Node.js mt checks do.
func (f *FinalACL) API(path string) *ACLAPI {
	if f == nil {
		return nil
	}
	if api, ok := f.APIs[path]; ok {
		return api
	}
	var found *ACLAPI
	for _, api := range f.APIsRegExp {
		if api.RegExp.MatchString(path) {
			found = api
		}
	}
	return found
}
