package native

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

type gqlResponse struct {
	Data   json.RawMessage `json:"data"`
	Errors []struct {
		Message    string         `json:"message"`
		Extensions map[string]any `json:"extensions"`
	} `json:"errors"`
}

func post(t *testing.T, query string) (int, gqlResponse) {
	t.Helper()
	srv := httptest.NewServer(NewHandler())
	defer srv.Close()
	body, _ := json.Marshal(map[string]string{"query": query})
	resp, err := http.Post(srv.URL, "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	var out gqlResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return resp.StatusCode, out
}

func TestViewerUnauthenticated(t *testing.T) {
	status, out := post(t, "{ viewer { id } }")
	if status != http.StatusOK {
		t.Fatalf("status = %d, want 200", status)
	}
	if string(out.Data) != "null" {
		t.Errorf("data = %s, want null", out.Data)
	}
	if len(out.Errors) == 0 {
		t.Fatal("no errors")
	}
	if out.Errors[0].Message != "Authentication required" {
		t.Errorf("message = %q", out.Errors[0].Message)
	}
	if out.Errors[0].Extensions["code"] != "AUTHENTICATION_ERROR" {
		t.Errorf("extensions.code = %v", out.Errors[0].Extensions["code"])
	}
}

func TestIntrospection(t *testing.T) {
	_, out := post(t, "{ __schema { queryType { name } } }")
	var data struct {
		Schema struct {
			QueryType struct{ Name string } `json:"queryType"`
		} `json:"__schema"`
	}
	if err := json.Unmarshal(out.Data, &data); err != nil {
		t.Fatalf("data: %v (errors %v)", err, out.Errors)
	}
	if data.Schema.QueryType.Name != "Query" {
		t.Errorf("queryType = %q, want Query", data.Schema.QueryType.Name)
	}
}
