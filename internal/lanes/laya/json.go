package laya

import (
	"encoding/json"
	"io"
	"net/http"
)

func decodeJSON(resp *http.Response, v any) error {
	return json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(v)
}
