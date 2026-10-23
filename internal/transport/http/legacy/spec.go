// Package legacy preserves the browser contract during the BFF cutover.
// openapi.json is a frozen export of the pre-BFF embedded specification.
package legacy

import (
	"github.com/getkin/kin-openapi/openapi3"

	_ "embed"
)

//go:embed openapi.json
var spec []byte

func GetSpec() (*openapi3.T, error) { return openapi3.NewLoader().LoadFromData(spec) }
