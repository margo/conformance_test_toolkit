package wfm

import "github.com/margo/dev-repo/shared-lib/crypto"

func WithRequestSigner(opt crypto.HTTPSigner) {}

func WithTls() {}

func WithNoTls() {}