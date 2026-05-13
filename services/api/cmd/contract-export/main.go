package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humachi"
	"github.com/go-chi/chi/v5"
	"gopkg.in/yaml.v3"

	idhttp "github.com/takalawang/corporate-catering-system/services/api/internal/identity/http"
	mhttp "github.com/takalawang/corporate-catering-system/services/api/internal/menu/http"
	ohttp "github.com/takalawang/corporate-catering-system/services/api/internal/order/http"
	payrollhttp "github.com/takalawang/corporate-catering-system/services/api/internal/payroll/http"
	qhttp "github.com/takalawang/corporate-catering-system/services/api/internal/quota/http"
	vhttp "github.com/takalawang/corporate-catering-system/services/api/internal/vendors/http"
)

func main() {
	r := chi.NewRouter()
	api := humachi.New(r, huma.DefaultConfig("T-Bite API", "0.1.0"))
	api.OpenAPI().Components.SecuritySchemes = map[string]*huma.SecurityScheme{
		"bearer": {Type: "http", Scheme: "bearer"},
	}
	// Register all routes; handlers must not be invoked during registration.
	(&idhttp.API{}).Register(api)
	(&vhttp.API{}).Register(api)
	(&mhttp.API{}).Register(api)
	(&qhttp.API{}).Register(api)
	(&ohttp.API{}).Register(api)
	(&payrollhttp.API{}).Register(api)

	j, err := api.OpenAPI().MarshalJSON()
	if err != nil {
		die(err)
	}

	// Normalize JSON output (sort + indent) for stable diffs.
	var doc map[string]any
	if err := json.Unmarshal(j, &doc); err != nil {
		die(err)
	}
	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		die(err)
	}
	if err := os.WriteFile("contract/openapi/openapi.json", append(out, '\n'), 0o644); err != nil {
		die(err)
	}

	y, err := yaml.Marshal(doc)
	if err != nil {
		die(err)
	}
	if err := os.WriteFile("contract/openapi/openapi.yaml", y, 0o644); err != nil {
		die(err)
	}

	fmt.Println("contract exported: openapi.json + openapi.yaml")
}

func die(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}
