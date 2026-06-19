package types

import (
	"github.com/archelab/sqlc-gen-python-arche/internal/core"
	"github.com/sqlc-dev/plugin-sdk-go/plugin"
)

type TypeConversionFunc func(req *plugin.GenerateRequest, col *plugin.Column, conf *core.Config) string
