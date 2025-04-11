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

func createMCPHandler(rcs *RefuncMCPServer, callType, callMethod, ns, fn string) func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		request.Params.Arguments["_call_type"] = callType
		request.Params.Arguments["_call_method"] = callMethod
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
				return mcp.NewToolResultText(string(bts)), nil
			}
		}
	}
}
