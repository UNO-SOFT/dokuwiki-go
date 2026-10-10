module github.com/UNO-SOFT/dokuwiki-go

go 1.27.0

tool (
	github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen
	github.com/tgulacsi/go/openapi-generator-cli
)

require (
	github.com/PuerkitoBio/goquery v1.13.0
	github.com/UNO-SOFT/cli v0.0.0-20261009074406-95b18dbd4953
	github.com/UNO-SOFT/zlog v0.8.6
	github.com/go-json-experiment/json v0.0.0-20260820222146-c27c302e5fc3
	github.com/google/renameio/v2 v2.0.2
	golang.org/x/sync v0.24.0
	golang.org/x/text v0.43.0
)

require (
	github.com/andybalholm/cascadia v1.3.5 // indirect
	github.com/dgryski/go-linebreak v0.0.0-20180812204043-d8f37254e7d3 // indirect
	github.com/dprotaso/go-yit v0.0.0-20220510233725-9ba8df137936 // indirect
	github.com/getkin/kin-openapi v0.142.0 // indirect
	github.com/go-logr/logr v1.4.4 // indirect
	github.com/go-openapi/jsonpointer v0.23.1 // indirect
	github.com/go-openapi/swag/jsonname v0.26.0 // indirect
	github.com/google/go-cmp v0.7.0 // indirect
	github.com/oapi-codegen/oapi-codegen/v2 v2.8.0 // indirect
	github.com/oasdiff/yaml v0.1.1 // indirect
	github.com/oasdiff/yaml3 v0.0.14 // indirect
	github.com/santhosh-tekuri/jsonschema/v6 v6.0.2 // indirect
	github.com/speakeasy-api/jsonpath v0.6.3 // indirect
	github.com/speakeasy-api/openapi v1.24.0 // indirect
	github.com/tgulacsi/go v0.28.13 // indirect
	github.com/vmware-labs/yaml-jsonpath v0.3.2 // indirect
	go.yaml.in/yaml/v3 v3.0.4 // indirect
	golang.org/x/exp v0.0.0-20261009195045-ca0d7ba23607 // indirect
	golang.org/x/mod v0.41.0 // indirect
	golang.org/x/net v0.61.0 // indirect
	golang.org/x/sys v0.49.0 // indirect
	golang.org/x/term v0.47.0 // indirect
	golang.org/x/tools v0.51.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

replace github.com/peterbourgon/ff/v4 v4.0.0-beta.1 => github.com/UNO-SOFT/ff/v4 v4.0.0-beta.1.us
