// SPDX-License-Identifier: AGPL-3.0

package dokuwiki

// go : generate curl -o dokuwiki.json -L -sS -m 30 "https://dokuwiki.org/lib/exe/openapi.php?spec=1"
// go : generate sed -i -e "/\"type\": \"null\"/ s/null/object/" dokuwiki.json
//go:generate go tool oapi-codegen -o dokuwiki_rest.gen.go -config cfg.yaml dokuwiki.json
// go : generate go tool openapi-generator-cli generate --package-name dokuwiki --minimal-update -i dokuwiki.json -g go
// go : generate rm -f go.*
//
// nix-shell -p steam-run --run "steam-run openapi-generator-cli kiota -l Go -o dw -n github.com/UNO-SOFT/dokuwiki/rest/dw -d dokuwiki.json"
// go : generate go tool openapi-generator-cli kiota -l Go -o dw -n github.com/UNO-SOFT/dokuwiki/rest/dw -d dokuwiki.json

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"reflect"
)

func (cl *Client) Do(req *http.Request) (*http.Response, error) {
	if err := cl.applyEditors(req.Context(), req, nil); err != nil {
		return nil, err
	}
	return cl.Client.Do(req)
}

func (cl ClientWithResponses) Do(req *http.Request) (*http.Response, error) {
	return cl.ClientInterface.(HttpRequestDoer).Do(req)
}

var ErrNotFound = errors.New("not found")

func CheckResponse(resp interface{ GetBody() []byte }) error {
	rv := reflect.ValueOf(resp)
	rm, ok := rv.Type().MethodByName("GetJSON200")
	if !ok {
		return fmt.Errorf("no GetJSON200 on %#v", resp)
	}
	// if resp.GetJSON200() != nil {
	if !rm.Func.Call([]reflect.Value{rv})[0].IsNil() {
		return nil
	}
	if b := resp.GetBody(); bytes.Contains(b, []byte("does not exist")) {
		return fmt.Errorf("%w: %s", ErrNotFound, string(b))
	} else {
		return fmt.Errorf("get %s", string(b))
	}
	return nil
}
