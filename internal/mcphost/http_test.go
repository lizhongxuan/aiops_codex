package mcphost

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

type testRoundTripFunc func(*http.Request) (*http.Response, error)

func (fn testRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func TestHTTPConnectionListToolsPreservesUIMeta(t *testing.T) {
	conn := &httpConnection{
		baseURL: "http://mcp.local",
		httpClient: &http.Client{
			Transport: testRoundTripFunc(func(req *http.Request) (*http.Response, error) {
				var rpcReq struct {
					Method string `json:"method"`
				}
				if err := json.NewDecoder(req.Body).Decode(&rpcReq); err != nil {
					t.Fatalf("decode request: %v", err)
				}
				if rpcReq.Method != "tools/list" {
					t.Fatalf("unexpected method %q", rpcReq.Method)
				}
				payload, err := json.Marshal(map[string]any{
					"jsonrpc": "2.0",
					"id":      1,
					"result": map[string]any{
						"tools": []map[string]any{
							{
								"name":        "topology",
								"description": "Render topology",
								"inputSchema": map[string]any{"type": "object"},
								"_meta": map[string]any{
									"ui": map[string]any{
										"resourceUri": "ui://coroot-rca/topology",
									},
								},
							},
						},
					},
				})
				if err != nil {
					t.Fatalf("marshal response: %v", err)
				}
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Body:       io.NopCloser(strings.NewReader(string(payload))),
					Request:    req,
				}, nil
			}),
		},
	}

	tools, err := conn.ListTools(context.Background())
	if err != nil {
		t.Fatalf("list tools: %v", err)
	}
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}
	uiMeta, _ := tools[0].Meta["ui"].(map[string]any)
	if got := uiMeta["resourceUri"]; got != "ui://coroot-rca/topology" {
		t.Fatalf("expected resourceUri to round-trip, got %#v", got)
	}
}

func TestHTTPConnectionReadResourceParsesMCPAppHTML(t *testing.T) {
	conn := &httpConnection{
		baseURL: "http://mcp.local",
		httpClient: &http.Client{
			Transport: testRoundTripFunc(func(req *http.Request) (*http.Response, error) {
				var rpcReq struct {
					Method string `json:"method"`
					Params struct {
						URI string `json:"uri"`
					} `json:"params"`
				}
				if err := json.NewDecoder(req.Body).Decode(&rpcReq); err != nil {
					t.Fatalf("decode request: %v", err)
				}
				if rpcReq.Method != "resources/read" {
					t.Fatalf("unexpected method %q", rpcReq.Method)
				}
				if rpcReq.Params.URI != "ui://coroot-rca/topology" {
					t.Fatalf("unexpected resource uri %q", rpcReq.Params.URI)
				}
				payload, err := json.Marshal(map[string]any{
					"jsonrpc": "2.0",
					"id":      1,
					"result": map[string]any{
						"contents": []map[string]any{
							{
								"uri":      "ui://coroot-rca/topology",
								"mimeType": "text/html;profile=mcp-app",
								"text":     "<!DOCTYPE html><html><body><h1>Topology</h1></body></html>",
							},
						},
					},
				})
				if err != nil {
					t.Fatalf("marshal response: %v", err)
				}
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": []string{"application/json"}},
					Body:       io.NopCloser(strings.NewReader(string(payload))),
					Request:    req,
				}, nil
			}),
		},
	}

	resp, err := conn.ReadResource(context.Background(), "ui://coroot-rca/topology")
	if err != nil {
		t.Fatalf("read resource: %v", err)
	}
	if len(resp.Contents) != 1 {
		t.Fatalf("expected 1 resource content, got %#v", resp.Contents)
	}
	if got := resp.Contents[0].MimeType; got != "text/html;profile=mcp-app" {
		t.Fatalf("unexpected mime type %q", got)
	}
	if got := resp.Contents[0].Text; got != "<!DOCTYPE html><html><body><h1>Topology</h1></body></html>" {
		t.Fatalf("unexpected html payload %q", got)
	}
}
