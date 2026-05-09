// Package proto contains generated protobuf definitions.
// This is a simplified version for compilation.
// In production, generate with: protoc --go_out=. --go-grpc_out=. agent.proto
package proto

import (
	"context"

	"google.golang.org/grpc"
)

// AgentServiceClient is the client API for AgentService service.
type AgentServiceClient interface {
	CreateAgent(ctx context.Context, in *CreateAgentRequest, opts ...grpc.CallOption) (*CreateAgentResponse, error)
	StartAgent(ctx context.Context, in *StartAgentRequest, opts ...grpc.CallOption) (*StartAgentResponse, error)
	StopAgent(ctx context.Context, in *StopAgentRequest, opts ...grpc.CallOption) (*StopAgentResponse, error)
	DeleteAgent(ctx context.Context, in *DeleteAgentRequest, opts ...grpc.CallOption) (*DeleteAgentResponse, error)
	GetAgentStatus(ctx context.Context, in *GetAgentStatusRequest, opts ...grpc.CallOption) (*GetAgentStatusResponse, error)
	ListAgents(ctx context.Context, in *ListAgentsRequest, opts ...grpc.CallOption) (*ListAgentsResponse, error)
	RunAgent(ctx context.Context, in *RunAgentRequest, opts ...grpc.CallOption) (AgentService_RunAgentClient, error)
	SendMessage(ctx context.Context, in *SendMessageRequest, opts ...grpc.CallOption) (*SendMessageResponse, error)
	HealthCheck(ctx context.Context, in *HealthCheckRequest, opts ...grpc.CallOption) (*HealthCheckResponse, error)
}

type agentServiceClient struct {
	cc grpc.ClientConnInterface
}

func NewAgentServiceClient(cc grpc.ClientConnInterface) AgentServiceClient {
	return &agentServiceClient{cc}
}

func (c *agentServiceClient) CreateAgent(ctx context.Context, in *CreateAgentRequest, opts ...grpc.CallOption) (*CreateAgentResponse, error) {
	out := new(CreateAgentResponse)
	err := c.cc.Invoke(ctx, "/aiagent.framework.v1.AgentService/CreateAgent", in, out, opts...)
	return out, err
}

func (c *agentServiceClient) StartAgent(ctx context.Context, in *StartAgentRequest, opts ...grpc.CallOption) (*StartAgentResponse, error) {
	out := new(StartAgentResponse)
	err := c.cc.Invoke(ctx, "/aiagent.framework.v1.AgentService/StartAgent", in, out, opts...)
	return out, err
}

func (c *agentServiceClient) StopAgent(ctx context.Context, in *StopAgentRequest, opts ...grpc.CallOption) (*StopAgentResponse, error) {
	out := new(StopAgentResponse)
	err := c.cc.Invoke(ctx, "/aiagent.framework.v1.AgentService/StopAgent", in, out, opts...)
	return out, err
}

func (c *agentServiceClient) DeleteAgent(ctx context.Context, in *DeleteAgentRequest, opts ...grpc.CallOption) (*DeleteAgentResponse, error) {
	out := new(DeleteAgentResponse)
	err := c.cc.Invoke(ctx, "/aiagent.framework.v1.AgentService/DeleteAgent", in, out, opts...)
	return out, err
}

func (c *agentServiceClient) GetAgentStatus(ctx context.Context, in *GetAgentStatusRequest, opts ...grpc.CallOption) (*GetAgentStatusResponse, error) {
	out := new(GetAgentStatusResponse)
	err := c.cc.Invoke(ctx, "/aiagent.framework.v1.AgentService/GetAgentStatus", in, out, opts...)
	return out, err
}

func (c *agentServiceClient) ListAgents(ctx context.Context, in *ListAgentsRequest, opts ...grpc.CallOption) (*ListAgentsResponse, error) {
	out := new(ListAgentsResponse)
	err := c.cc.Invoke(ctx, "/aiagent.framework.v1.AgentService/ListAgents", in, out, opts...)
	return out, err
}

func (c *agentServiceClient) RunAgent(ctx context.Context, in *RunAgentRequest, opts ...grpc.CallOption) (AgentService_RunAgentClient, error) {
	stream, err := c.cc.NewStream(ctx, &AgentService_ServiceDesc.Streams[0], "/aiagent.framework.v1.AgentService/RunAgent", opts...)
	if err != nil {
		return nil, err
	}
	x := &agentServiceRunAgentClient{stream}
	if err := x.ClientStream.SendMsg(in); err != nil {
		return nil, err
	}
	if err := x.ClientStream.CloseSend(); err != nil {
		return nil, err
	}
	return x, nil
}

type AgentService_RunAgentClient interface {
	Recv() (*AgentEvent, error)
	grpc.ClientStream
}

type agentServiceRunAgentClient struct {
	grpc.ClientStream
}

func (x *agentServiceRunAgentClient) Recv() (*AgentEvent, error) {
	m := new(AgentEvent)
	if err := x.ClientStream.RecvMsg(m); err != nil {
		return nil, err
	}
	return m, nil
}

func (c *agentServiceClient) SendMessage(ctx context.Context, in *SendMessageRequest, opts ...grpc.CallOption) (*SendMessageResponse, error) {
	out := new(SendMessageResponse)
	err := c.cc.Invoke(ctx, "/aiagent.framework.v1.AgentService/SendMessage", in, out, opts...)
	return out, err
}

func (c *agentServiceClient) HealthCheck(ctx context.Context, in *HealthCheckRequest, opts ...grpc.CallOption) (*HealthCheckResponse, error) {
	out := new(HealthCheckResponse)
	err := c.cc.Invoke(ctx, "/aiagent.framework.v1.AgentService/HealthCheck", in, out, opts...)
	return out, err
}

// AgentServiceServer is the server API for AgentService service.
type AgentServiceServer interface {
	CreateAgent(context.Context, *CreateAgentRequest) (*CreateAgentResponse, error)
	StartAgent(context.Context, *StartAgentRequest) (*StartAgentResponse, error)
	StopAgent(context.Context, *StopAgentRequest) (*StopAgentResponse, error)
	DeleteAgent(context.Context, *DeleteAgentRequest) (*DeleteAgentResponse, error)
	GetAgentStatus(context.Context, *GetAgentStatusRequest) (*GetAgentStatusResponse, error)
	ListAgents(context.Context, *ListAgentsRequest) (*ListAgentsResponse, error)
	RunAgent(*RunAgentRequest, AgentService_RunAgentServer) error
	SendMessage(context.Context, *SendMessageRequest) (*SendMessageResponse, error)
	HealthCheck(context.Context, *HealthCheckRequest) (*HealthCheckResponse, error)
}

type UnimplementedAgentServiceServer struct{}

func (UnimplementedAgentServiceServer) CreateAgent(context.Context, *CreateAgentRequest) (*CreateAgentResponse, error) {
	return nil, nil
}
func (UnimplementedAgentServiceServer) StartAgent(context.Context, *StartAgentRequest) (*StartAgentResponse, error) {
	return nil, nil
}
func (UnimplementedAgentServiceServer) StopAgent(context.Context, *StopAgentRequest) (*StopAgentResponse, error) {
	return nil, nil
}
func (UnimplementedAgentServiceServer) DeleteAgent(context.Context, *DeleteAgentRequest) (*DeleteAgentResponse, error) {
	return nil, nil
}
func (UnimplementedAgentServiceServer) GetAgentStatus(context.Context, *GetAgentStatusRequest) (*GetAgentStatusResponse, error) {
	return nil, nil
}
func (UnimplementedAgentServiceServer) ListAgents(context.Context, *ListAgentsRequest) (*ListAgentsResponse, error) {
	return nil, nil
}
func (UnimplementedAgentServiceServer) RunAgent(*RunAgentRequest, AgentService_RunAgentServer) error {
	return nil
}
func (UnimplementedAgentServiceServer) SendMessage(context.Context, *SendMessageRequest) (*SendMessageResponse, error) {
	return nil, nil
}
func (UnimplementedAgentServiceServer) HealthCheck(context.Context, *HealthCheckRequest) (*HealthCheckResponse, error) {
	return nil, nil
}

func RegisterAgentServiceServer(s grpc.ServiceRegistrar, srv AgentServiceServer) {
	s.RegisterService(&AgentService_ServiceDesc, srv)
}

type AgentService_RunAgentServer interface {
	Send(*AgentEvent) error
	grpc.ServerStream
}

var AgentService_ServiceDesc = grpc.ServiceDesc{
	ServiceName: "aiagent.framework.v1.AgentService",
	HandlerType: (*AgentServiceServer)(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: "CreateAgent",
			Handler:    _AgentService_CreateAgent_Handler,
		},
		{
			MethodName: "StartAgent",
			Handler:    _AgentService_StartAgent_Handler,
		},
		{
			MethodName: "StopAgent",
			Handler:    _AgentService_StopAgent_Handler,
		},
		{
			MethodName: "DeleteAgent",
			Handler:    _AgentService_DeleteAgent_Handler,
		},
		{
			MethodName: "GetAgentStatus",
			Handler:    _AgentService_GetAgentStatus_Handler,
		},
		{
			MethodName: "ListAgents",
			Handler:    _AgentService_ListAgents_Handler,
		},
		{
			MethodName: "SendMessage",
			Handler:    _AgentService_SendMessage_Handler,
		},
		{
			MethodName: "HealthCheck",
			Handler:    _AgentService_HealthCheck_Handler,
		},
	},
	Streams: []grpc.StreamDesc{
		{
			StreamName:    "RunAgent",
			Handler:       _AgentService_RunAgent_Handler,
			ServerStreams: true,
		},
	},
}

func _AgentService_CreateAgent_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(CreateAgentRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	return srv.(AgentServiceServer).CreateAgent(ctx, in)
}

func _AgentService_StartAgent_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(StartAgentRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	return srv.(AgentServiceServer).StartAgent(ctx, in)
}

func _AgentService_StopAgent_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(StopAgentRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	return srv.(AgentServiceServer).StopAgent(ctx, in)
}

func _AgentService_DeleteAgent_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(DeleteAgentRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	return srv.(AgentServiceServer).DeleteAgent(ctx, in)
}

func _AgentService_GetAgentStatus_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(GetAgentStatusRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	return srv.(AgentServiceServer).GetAgentStatus(ctx, in)
}

func _AgentService_ListAgents_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(ListAgentsRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	return srv.(AgentServiceServer).ListAgents(ctx, in)
}

func _AgentService_SendMessage_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(SendMessageRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	return srv.(AgentServiceServer).SendMessage(ctx, in)
}

func _AgentService_HealthCheck_Handler(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
	in := new(HealthCheckRequest)
	if err := dec(in); err != nil {
		return nil, err
	}
	return srv.(AgentServiceServer).HealthCheck(ctx, in)
}

func _AgentService_RunAgent_Handler(srv interface{}, stream grpc.ServerStream) error {
	in := new(RunAgentRequest)
	if err := stream.RecvMsg(in); err != nil {
		return err
	}
	// Create wrapper that implements AgentService_RunAgentServer
	wrapper := &runAgentServerWrapper{stream}
	return srv.(AgentServiceServer).RunAgent(in, wrapper)
}

// runAgentServerWrapper wraps grpc.ServerStream to implement AgentService_RunAgentServer
type runAgentServerWrapper struct {
	grpc.ServerStream
}

func (w *runAgentServerWrapper) Send(e *AgentEvent) error {
	return w.ServerStream.SendMsg(e)
}

// Type definitions from agent.proto (simplified)

type CreateAgentRequest struct {
	AgentId     string
	Name        string
	Description string
	Model       string
	Type        AgentType
	Instruction string
	Tools       []*ToolConfig
	SubAgentIds []string
	Harness     *HarnessConfig
}

type CreateAgentResponse struct {
	AgentId string
	Success bool
	Error   string
}

type AgentType int32

const (
	AgentType_AGENT_TYPE_UNSPECIFIED AgentType = 0
	AgentType_AGENT_TYPE_LLM         AgentType = 1
	AgentType_AGENT_TYPE_SEQUENTIAL  AgentType = 2
	AgentType_AGENT_TYPE_PARALLEL    AgentType = 3
	AgentType_AGENT_TYPE_LOOP        AgentType = 4
	AgentType_AGENT_TYPE_CUSTOM      AgentType = 5
)

type ToolConfig struct {
	Name        string
	Description string
	Enabled     bool
	Config      []byte
}

type HarnessConfig struct {
	Model   *ModelHarnessConfig
	Memory  *MemoryHarnessConfig
	Sandbox *SandboxHarnessConfig
	Skills  *SkillsHarnessConfig
}

type ModelHarnessConfig struct {
	Provider     string
	Endpoint     string
	DefaultModel string
	Models       []*ModelConfig
}

type ModelConfig struct {
	Name        string
	Allowed     bool
	MaxTokens   int32
	Temperature float64
}

type MemoryHarnessConfig struct {
	Type     string
	Endpoint string
	Ttl      int32
}

type SandboxHarnessConfig struct {
	Type     string
	Mode     string
	Endpoint string
	Timeout  int32
}

type SkillsHarnessConfig struct {
	Skills []*SkillConfig
}

type SkillConfig struct {
	Name    string
	Version string
	Allowed bool
}

type StartAgentRequest struct {
	AgentId string
}

type StartAgentResponse struct {
	Success bool
	Error   string
}

type StopAgentRequest struct {
	AgentId string
}

type StopAgentResponse struct {
	Success bool
	Error   string
}

type DeleteAgentRequest struct {
	AgentId string
}

type DeleteAgentResponse struct {
	Success bool
	Error   string
}

type GetAgentStatusRequest struct {
	AgentId string
}

type GetAgentStatusResponse struct {
	AgentId          string
	Name             string
	Phase            AgentPhase
	Running          bool
	SessionCount     int64
	LastActivityTime int64
	Metrics          *AgentMetrics
}

type AgentPhase int32

const (
	AgentPhase_AGENT_PHASE_UNSPECIFIED   AgentPhase = 0
	AgentPhase_AGENT_PHASE_PENDING       AgentPhase = 1
	AgentPhase_AGENT_PHASE_INITIALIZING  AgentPhase = 2
	AgentPhase_AGENT_PHASE_RUNNING       AgentPhase = 3
	AgentPhase_AGENT_PHASE_MIGRATING     AgentPhase = 4
	AgentPhase_AGENT_PHASE_ERROR         AgentPhase = 5
	AgentPhase_AGENT_PHASE_TERMINATED    AgentPhase = 6
)

type AgentMetrics struct {
	TotalInvocations           int64
	SuccessfulInvocations      int64
	FailedInvocations          int64
	AverageLatency             float64
	TokensUsed                 int64
}

type ListAgentsRequest struct{}

type ListAgentsResponse struct {
	Agents []*AgentInfo
}

type AgentInfo struct {
	Id    string
	Name  string
	Type  AgentType
	Phase AgentPhase
}

type RunAgentRequest struct {
	AgentId      string
	SessionId    string
	InvocationId string
	UserContent  *Content
	Config       *RunConfig
}

type Content struct {
	Type     string
	Text     string
	Data     []byte
	MimeType string
}

type RunConfig struct {
	MaxIterations       int32
	TemperatureOverride float64
	CustomConfig        map[string]string
}

type AgentEvent struct {
	InvocationId string
	Author       string
	Type         EventType
	Content      *Content
	Error        string
	Timestamp    int64
	Metadata     map[string]string
}

type EventType int32

const (
	EventType_EVENT_TYPE_UNSPECIFIED    EventType = 0
	EventType_EVENT_TYPE_TEXT           EventType = 1
	EventType_EVENT_TYPE_TOOL_CALL      EventType = 2
	EventType_EVENT_TYPE_TOOL_RESULT    EventType = 3
	EventType_EVENT_TYPE_MODEL_REQUEST  EventType = 4
	EventType_EVENT_TYPE_MODEL_RESPONSE EventType = 5
	EventType_EVENT_TYPE_STATE_CHANGE   EventType = 6
	EventType_EVENT_TYPE_ERROR          EventType = 7
	EventType_EVENT_TYPE_COMPLETE       EventType = 8
)

type SendMessageRequest struct {
	AgentId   string
	SessionId string
	Content   *Content
}

type SendMessageResponse struct {
	Response  *Content
	SessionId string
	Error     string
}

type HealthCheckRequest struct{}

type HealthCheckResponse struct {
	Healthy       bool
	Version       string
	UptimeSeconds int64
	AgentCount    int32
}