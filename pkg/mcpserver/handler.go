package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/refunc/refunc/pkg/client"
	"github.com/refunc/refunc/pkg/messages"
	rfutils "github.com/refunc/refunc/pkg/utils"
	"k8s.io/klog/v2"
)

func createMCPHandler(rcs *RefuncMCPServer, cfg toolConfig, callType, ns, fn string) func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		args := request.GetArguments()
		args["_call_type"] = callType
		args["_call_method"] = cfg.Name
		request.Params.Arguments = args
		payload, err := json.Marshal(request.Params.Arguments)
		if err != nil {
			return nil, errors.New("call func payload parse error")
		}
		invokeRequest := &messages.InvokeRequest{
			Args:      payload,
			RequestID: rfutils.GenID(payload),
		}
		fndef, err := rcs.funcdefLister.Funcdeves(ns).Get(fn)
		if err != nil {
			return nil, err
		}
		endpoint := fndef.Namespace + "/" + fndef.Name
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		ctx = client.WithLogger(ctx, klog.V(1))
		ctx = client.WithNatsConn(ctx, rcs.natsConn)
		ctx = client.WithTimeoutHint(ctx, time.Duration(fndef.Spec.Runtime.Timeout)*time.Second)
		ctx = client.WithLoggingHint(ctx, false)
		taskr, err := client.NewTaskResolver(ctx, endpoint, invokeRequest)
		if err != nil {
			klog.Error(err)
			return nil, fmt.Errorf("call func error %v", err)
		}
		for {
			select {
			case <-ctx.Done():
				return nil, errors.New("call func timeout")
			case <-taskr.Done():
				bts, err := taskr.Result()
				if err != nil {
					bts = messages.GetErrActionBytes(err)
				}
				var v interface{}
				// lambda result always is json
				if err := json.Unmarshal(bts, &v); err != nil {
					return nil, err
				}
				// reencode jsonstr as utf-8 with golang internel default
				// not use unicode escape as jsonstr to save llm token
				bts, _ = json.Marshal(v)
				res := mcp.NewToolResultText(string(bts))
				if cfg.ResultWithStructured {
					// option disable result with structured content
					res.StructuredContent = v
				}
				return res, nil
			}
		}
	}
}
