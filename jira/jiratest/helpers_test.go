package jiratest_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/git-bug/git-bug/jira/jiratest"
)

// resp is a raw response: the tests read JSON as a client would, with no
// types shared with the fake.
type resp struct {
	status int
	header http.Header
	body   []byte
}

func (r resp) obj(t *testing.T) map[string]any {
	t.Helper()
	var m map[string]any
	require.NoError(t, json.Unmarshal(r.body, &m), string(r.body))
	return m
}

func (r resp) arr(t *testing.T) []any {
	t.Helper()
	var a []any
	require.NoError(t, json.Unmarshal(r.body, &a), string(r.body))
	return a
}

type reqOpt func(*http.Request)

func noAuth(r *http.Request) { r.Header.Del("Authorization") }

func withAuth(v string) reqOpt { return func(r *http.Request) { r.Header.Set("Authorization", v) } }

// do sends a request as Site.Me. body is marshalled unless it is a string
// or []byte, which go as they are.
func do(t *testing.T, s *jiratest.Server, method, path string, body any, opts ...reqOpt) resp {
	t.Helper()
	var rd io.Reader
	switch b := body.(type) {
	case nil:
	case string:
		rd = bytes.NewBufferString(b)
	case []byte:
		rd = bytes.NewReader(b)
	default:
		raw, err := json.Marshal(b)
		require.NoError(t, err)
		rd = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, s.URL()+path, rd)
	require.NoError(t, err)
	req.Header.Set("Authorization", s.AuthHeader())
	req.Header.Set("Accept", "application/json")
	if rd != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for _, o := range opts {
		o(req)
	}
	res, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	require.NoError(t, err)
	return resp{status: res.StatusCode, header: res.Header, body: raw}
}

func get(t *testing.T, s *jiratest.Server, path string, opts ...reqOpt) resp {
	t.Helper()
	return do(t, s, http.MethodGet, path, nil, opts...)
}

// search is POST /search/jql, the one form the client sends, with a body
// built from params: lists stay lists, expand is comma-delimited, and
// maxResults and reconcileIssues are numbers.
func search(t *testing.T, s *jiratest.Server, params url.Values, opts ...reqOpt) resp {
	t.Helper()
	body := map[string]any{}
	for k, v := range params {
		switch k {
		case "jql", "nextPageToken":
			body[k] = v[0]
		case "expand":
			body[k] = strings.Join(v, ",")
		case "maxResults":
			n, err := strconv.Atoi(v[0])
			require.NoError(t, err)
			body[k] = n
		case "reconcileIssues":
			var ids []int
			for _, id := range strings.Split(strings.Join(v, ","), ",") {
				n, err := strconv.Atoi(id)
				require.NoError(t, err)
				ids = append(ids, n)
			}
			body[k] = ids
		default:
			body[k] = v
		}
	}
	return do(t, s, http.MethodPost, "/rest/api/3/search/jql", body, opts...)
}

func keysOf(t *testing.T, page map[string]any) []string {
	t.Helper()
	var keys []string
	for _, i := range page["issues"].([]any) {
		keys = append(keys, i.(map[string]any)["key"].(string))
	}
	return keys
}

// newServer is a company site whose index does not lag, for the tests
// that are not about the lag.
func newServer(t *testing.T, opts ...jiratest.Option) *jiratest.Server {
	return jiratest.New(t, append([]jiratest.Option{jiratest.WithIndexLag(0, 0)}, opts...)...)
}

func errorBody(t *testing.T, r resp) (messages []any, errs map[string]any) {
	t.Helper()
	m := r.obj(t)
	require.Contains(t, m, "errorMessages")
	require.Contains(t, m, "errors")
	return m["errorMessages"].([]any), m["errors"].(map[string]any)
}

func path(m any, keys ...string) any {
	for _, k := range keys {
		mm, ok := m.(map[string]any)
		if !ok {
			return nil
		}
		m = mm[k]
	}
	return m
}

func adf(text string) map[string]any {
	return map[string]any{"type": "doc", "version": 1, "content": []any{
		map[string]any{"type": "paragraph", "content": []any{map[string]any{"type": "text", "text": text}}},
	}}
}
