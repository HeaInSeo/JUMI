package api

import (
	"context"
	"encoding/json"

	"google.golang.org/grpc"
	"google.golang.org/grpc/encoding"
)

const ServiceName = "jumi.v1.RunService"

type RunServiceServer interface {
	SubmitRun(context.Context, SubmitRunRequest) (SubmitRunResponse, error)
	GetRun(context.Context, GetRunRequest) (GetRunResponse, error)
	ListRunNodes(context.Context, ListRunNodesRequest) (ListRunNodesResponse, error)
	CancelRun(context.Context, CancelRunRequest) (CancelRunResponse, error)
}

type jsonCodec struct{}

func (jsonCodec) Marshal(v any) ([]byte, error) {
	return json.Marshal(v)
}

func (jsonCodec) Unmarshal(data []byte, v any) error {
	return json.Unmarshal(data, v)
}

func (jsonCodec) Name() string {
	return "json"
}

func init() {
	encoding.RegisterCodec(jsonCodec{})
}

func RegisterRunService(server *grpc.Server, service *Service) {
	server.RegisterService(&grpc.ServiceDesc{
		ServiceName: ServiceName,
		HandlerType: (*RunServiceServer)(nil),
		Methods: []grpc.MethodDesc{
			{MethodName: "SubmitRun", Handler: submitRunHandler(service)},
			{MethodName: "GetRun", Handler: getRunHandler(service)},
			{MethodName: "ListRunNodes", Handler: listRunNodesHandler(service)},
			{MethodName: "CancelRun", Handler: cancelRunHandler(service)},
		},
		Streams:  []grpc.StreamDesc{},
		Metadata: "docs/JUMI_GRPC_CONTRACT_DRAFT.ko.md",
	}, service)
}

type RunServiceClient struct {
	conn grpc.ClientConnInterface
}

func NewRunServiceClient(conn grpc.ClientConnInterface) *RunServiceClient {
	return &RunServiceClient{conn: conn}
}

func (c *RunServiceClient) SubmitRun(ctx context.Context, req *SubmitRunRequest, opts ...grpc.CallOption) (*SubmitRunResponse, error) {
	resp := new(SubmitRunResponse)
	if err := c.conn.Invoke(ctx, "/"+ServiceName+"/SubmitRun", req, resp, opts...); err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *RunServiceClient) GetRun(ctx context.Context, req *GetRunRequest, opts ...grpc.CallOption) (*GetRunResponse, error) {
	resp := new(GetRunResponse)
	if err := c.conn.Invoke(ctx, "/"+ServiceName+"/GetRun", req, resp, opts...); err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *RunServiceClient) ListRunNodes(ctx context.Context, req *ListRunNodesRequest, opts ...grpc.CallOption) (*ListRunNodesResponse, error) {
	resp := new(ListRunNodesResponse)
	if err := c.conn.Invoke(ctx, "/"+ServiceName+"/ListRunNodes", req, resp, opts...); err != nil {
		return nil, err
	}
	return resp, nil
}

func (c *RunServiceClient) CancelRun(ctx context.Context, req *CancelRunRequest, opts ...grpc.CallOption) (*CancelRunResponse, error) {
	resp := new(CancelRunResponse)
	if err := c.conn.Invoke(ctx, "/"+ServiceName+"/CancelRun", req, resp, opts...); err != nil {
		return nil, err
	}
	return resp, nil
}

func submitRunHandler(service *Service) grpc.MethodHandler {
	return func(srv any, ctx context.Context, dec func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
		in := new(SubmitRunRequest)
		if err := dec(in); err != nil {
			return nil, err
		}
		if interceptor == nil {
			return service.SubmitRun(ctx, *in)
		}
		info := &grpc.UnaryServerInfo{Server: srv, FullMethod: "/" + ServiceName + "/SubmitRun"}
		handler := func(ctx context.Context, req any) (any, error) {
			return service.SubmitRun(ctx, *(req.(*SubmitRunRequest)))
		}
		return interceptor(ctx, in, info, handler)
	}
}

func getRunHandler(service *Service) grpc.MethodHandler {
	return func(srv any, ctx context.Context, dec func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
		in := new(GetRunRequest)
		if err := dec(in); err != nil {
			return nil, err
		}
		if interceptor == nil {
			return service.GetRun(ctx, *in)
		}
		info := &grpc.UnaryServerInfo{Server: srv, FullMethod: "/" + ServiceName + "/GetRun"}
		handler := func(ctx context.Context, req any) (any, error) {
			return service.GetRun(ctx, *(req.(*GetRunRequest)))
		}
		return interceptor(ctx, in, info, handler)
	}
}

func listRunNodesHandler(service *Service) grpc.MethodHandler {
	return func(srv any, ctx context.Context, dec func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
		in := new(ListRunNodesRequest)
		if err := dec(in); err != nil {
			return nil, err
		}
		if interceptor == nil {
			return service.ListRunNodes(ctx, *in)
		}
		info := &grpc.UnaryServerInfo{Server: srv, FullMethod: "/" + ServiceName + "/ListRunNodes"}
		handler := func(ctx context.Context, req any) (any, error) {
			return service.ListRunNodes(ctx, *(req.(*ListRunNodesRequest)))
		}
		return interceptor(ctx, in, info, handler)
	}
}

func cancelRunHandler(service *Service) grpc.MethodHandler {
	return func(srv any, ctx context.Context, dec func(any) error, interceptor grpc.UnaryServerInterceptor) (any, error) {
		in := new(CancelRunRequest)
		if err := dec(in); err != nil {
			return nil, err
		}
		if interceptor == nil {
			return service.CancelRun(ctx, *in)
		}
		info := &grpc.UnaryServerInfo{Server: srv, FullMethod: "/" + ServiceName + "/CancelRun"}
		handler := func(ctx context.Context, req any) (any, error) {
			return service.CancelRun(ctx, *(req.(*CancelRunRequest)))
		}
		return interceptor(ctx, in, info, handler)
	}
}
