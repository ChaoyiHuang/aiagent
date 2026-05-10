// Package v1 contains API Schema definitions for the agent.ai API group.
// +k8s:deepcopy-gen=package
// +groupName=agent.ai
package v1

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/scheme"
)

var (
	// GroupVersion is the group version for the AI API.
	GroupVersion = schema.GroupVersion{Group: "agent.ai", Version: "v1"}

	// SchemeBuilder creates a Scheme that registers the types for this API group.
	SchemeBuilder = &scheme.Builder{GroupVersion: GroupVersion}

	// AddToScheme adds the types in this group to the given scheme.
	AddToScheme = SchemeBuilder.AddToScheme
)

// SchemeGroupVersion is the identifier for the API group and version.
// This is used for registering custom resources.
var SchemeGroupVersion = GroupVersion

// Resource takes an unqualified resource and returns a Group qualified GroupResource.
func Resource(resource string) schema.GroupResource {
	return GroupVersion.WithResource(resource).GroupResource()
}

func init() {
	// Register all custom resource types
	SchemeBuilder.Register(&AIAgent{}, &AIAgentList{})
	SchemeBuilder.Register(&AgentRuntime{}, &AgentRuntimeList{})
	SchemeBuilder.Register(&Harness{}, &HarnessList{})
}