package models

import "github.com/margo/dev-repo/non-standard/generatedCode/wfm/nbi"

type AppPkg struct {
	Id          string
	Op          AppPkgOp
	OpState     AppPkgOpStatus
	Description *nbi.AppDescription // mandatory field
	Resources   map[string][]byte   // omitempty, *ApplicationResources  // optional field //map[string][]byte // filename -> content
}

type ApplicationResources struct {
	// icon, releasenotes, license file..
}
