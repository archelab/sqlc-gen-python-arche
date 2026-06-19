package main

import (
	python "github.com/archelab/sqlc-gen-python-arche/internal"
	"github.com/sqlc-dev/plugin-sdk-go/codegen"
)

func main() {
	codegen.Run(python.Generate)
}
