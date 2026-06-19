package codegen

import (
	"github.com/archelab/sqlc-gen-python-arche/internal/codegen/builders"
	"github.com/sqlc-dev/plugin-sdk-go/plugin"
)

func (dr *Driver) BuildInitFile() *plugin.File {
	body := builders.NewIndentStringBuilder(dr.conf.IndentChar, dr.conf.CharsPerIndentLevel)
	dr.writeFileHeader(body)
	body.WriteSqlcHeader()
	body.WriteInitFileModuleDocstring()
	return &plugin.File{
		Name:     "__init__.py",
		Contents: []byte(body.String()),
	}
}
