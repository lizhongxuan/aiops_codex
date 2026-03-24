// Code generated from proto/agent.proto. DO NOT EDIT.

package agentrpc

import (
	"context"

	"google.golang.org/grpc"
)

const ServiceName = "aiops.agent.v1.AgentService"

type AgentServiceServer interface {
	Connect(AgentService_ConnectServer) error
}

type AgentServiceClient interface {
	Connect(ctx context.Context, opts ...grpc.CallOption) (AgentService_ConnectClient, error)
}

type AgentService_ConnectServer interface {
	Send(*Envelope) error
	Recv() (*Envelope, error)
	grpc.ServerStream
}

type AgentService_ConnectClient interface {
	Send(*Envelope) error
	Recv() (*Envelope, error)
	grpc.ClientStream
}

type agentServiceClient struct {
	cc grpc.ClientConnInterface
}

func NewAgentServiceClient(cc grpc.ClientConnInterface) AgentServiceClient {
	return &agentServiceClient{cc: cc}
}

func (c *agentServiceClient) Connect(ctx context.Context, opts ...grpc.CallOption) (AgentService_ConnectClient, error) {
	stream, err := c.cc.NewStream(ctx, &AgentService_ServiceDesc.Streams[0], "/"+ServiceName+"/Connect", opts...)
	if err != nil {
		return nil, err
	}
	return &agentServiceConnectClient{ClientStream: stream}, nil
}

type agentServiceConnectClient struct {
	grpc.ClientStream
}

func (c *agentServiceConnectClient) Send(msg *Envelope) error {
	return c.ClientStream.SendMsg(msg)
}

func (c *agentServiceConnectClient) Recv() (*Envelope, error) {
	msg := new(Envelope)
	if err := c.ClientStream.RecvMsg(msg); err != nil {
		return nil, err
	}
	return msg, nil
}

type agentServiceConnectServer struct {
	grpc.ServerStream
}

func (s *agentServiceConnectServer) Send(msg *Envelope) error {
	return s.ServerStream.SendMsg(msg)
}

func (s *agentServiceConnectServer) Recv() (*Envelope, error) {
	msg := new(Envelope)
	if err := s.ServerStream.RecvMsg(msg); err != nil {
		return nil, err
	}
	return msg, nil
}

func RegisterAgentServiceServer(registrar grpc.ServiceRegistrar, srv AgentServiceServer) {
	registrar.RegisterService(&AgentService_ServiceDesc, srv)
}

var AgentService_ServiceDesc = grpc.ServiceDesc{
	ServiceName: ServiceName,
	HandlerType: (*AgentServiceServer)(nil),
	Streams: []grpc.StreamDesc{
		{
			StreamName:    "Connect",
			Handler:       _AgentService_Connect_Handler,
			ServerStreams: true,
			ClientStreams: true,
		},
	},
}

func _AgentService_Connect_Handler(srv any, stream grpc.ServerStream) error {
	return srv.(AgentServiceServer).Connect(&agentServiceConnectServer{ServerStream: stream})
}
