package dokuwiki

// go : generate go tool openapi-generator-cli generate --package-name dokuwiki --minimal-update -i dokuwiki.json -g go
// go : generate rm -f go.*
//
// nix-shell -p steam-run --run "steam-run openapi-generator-cli kiota -l Go -o dw -n github.com/UNO-SOFT/dokuwiki/rest/dw -d dokuwiki.json"
//go:generate go tool openapi-generator-cli kiota -l Go -o dw -n github.com/UNO-SOFT/dokuwiki/rest/dw -d dokuwiki.json
