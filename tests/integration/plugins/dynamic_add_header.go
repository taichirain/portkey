//go:build ignore
// +build ignore

package main

import (
	"net/http"

	"github.com/taichirain/portkey/internal/data/plugin"
)

type DynamicAddHeaderPlugin struct {
	config map[string]interface{}
}

func (p *DynamicAddHeaderPlugin) Name() string {
	return "dynamic_add_header"
}

func (p *DynamicAddHeaderPlugin) OnRequest(ctx *plugin.PluginContext) error {
	headerName := "X-Dynamic-Plugin"
	headerValue := "added-by-dynamic-plugin"

	if hName, ok := p.config["header_name"].(string); ok && hName != "" {
		headerName = hName
	}
	if hValue, ok := p.config["header_value"].(string); ok && hValue != "" {
		headerValue = hValue
	}

	ctx.Request.Header.Set(headerName, headerValue)
	return nil
}

func (p *DynamicAddHeaderPlugin) OnResponse(ctx *plugin.PluginContext, resp *http.Response) error {
	if resp != nil {
		resp.Header.Set("X-Dynamic-Plugin-Executed", "true")
	}
	return nil
}

func (p *DynamicAddHeaderPlugin) OnError(ctx *plugin.PluginContext, err error) error {
	return nil
}

type DynamicAddHeaderFactory struct{}

func (f *DynamicAddHeaderFactory) Name() string {
	return "dynamic_add_header"
}

func (f *DynamicAddHeaderFactory) Create(config map[string]interface{}) (plugin.Plugin, error) {
	return &DynamicAddHeaderPlugin{config: config}, nil
}

var PluginFactory plugin.PluginFactory = &DynamicAddHeaderFactory{}
