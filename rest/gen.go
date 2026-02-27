package dokuwiki

//go:generate go tool openapi-generator-cli generate --package-name dokuwiki --minimal-update -i dokuwiki.json -g go
//go:generate rm -f go.*
