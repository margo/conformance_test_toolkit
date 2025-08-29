package models

type AppPkgOpStatus string

const (
	AppPkgOpStatusSuccess AppPkgOpStatus = "SUCCESS"
	AppPkgOpStatusFailed  AppPkgOpStatus = "FAILED"
	AppPkgOpStatusPending AppPkgOpStatus = "PENDING"
	AppPkgOpStatusUnknown AppPkgOpStatus = "UNKNOWN"
)

type AppPkgOp string

const (
	AppPkgOpOnboard AppPkgOp = "ONBOARD"
	AppPkgOpDeboard AppPkgOp = "DEBOARD"
	AppPkgOpStage   AppPkgOp = "STAGE"
	AppPkgOpUnstage AppPkgOp = "UNSTAGE"
)
