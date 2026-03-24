package agentrpc

import (
	"context"
	"encoding/json"

	"google.golang.org/grpc"
	"google.golang.org/grpc/encoding"
)

const ServiceName = "aiops.agent.v1.AgentService"

func init() {
	encoding.RegisterCodec(jsonCodec{})
}

type jsonCodec struct{}

func (jsonCodec) Name() string {
	return "json"
}

func (jsonCodec) Marshal(v any) ([]byte, error) {
	return json.Marshal(v)
}

func (jsonCodec) Unmarshal(data []byte, v any) error {
	return json.Unmarshal(data, v)
}

type Registration struct {
	Token        string            `json:"token,omitempty"`
	HostID       string            `json:"hostId,omitempty"`
	Hostname     string            `json:"hostname,omitempty"`
	OS           string            `json:"os,omitempty"`
	Arch         string            `json:"arch,omitempty"`
	AgentVersion string            `json:"agentVersion,omitempty"`
	Labels       map[string]string `json:"labels,omitempty"`
}

type Heartbeat struct {
	HostID    string `json:"hostId,omitempty"`
	Timestamp int64  `json:"timestamp,omitempty"`
}

type Ping struct {
	Message   string `json:"message,omitempty"`
	Timestamp int64  `json:"timestamp,omitempty"`
}

type Ack struct {
	Message   string `json:"message,omitempty"`
	Timestamp int64  `json:"timestamp,omitempty"`
}

type Envelope struct {
	Kind         string        `json:"kind"`
	Registration *Registration `json:"registration,omitempty"`
	Heartbeat    *Heartbeat    `json:"heartbeat,omitempty"`
	Ping         *Ping         `json:"ping,omitempty"`
	Ack          *Ack          `json:"ack,omitempty"`
	Error        string        `json:"error,omitempty"`
}

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
