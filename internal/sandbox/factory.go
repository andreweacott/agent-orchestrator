package sandbox

import (
	"fmt"
	"os"
)

// ProviderType represents the type of sandbox provider
type ProviderType string

const (
	// ProviderTypeDocker uses Docker containers for sandboxes
	ProviderTypeDocker ProviderType = "docker"
	// ProviderTypeKubernetes uses Kubernetes pods for sandboxes
	ProviderTypeKubernetes ProviderType = "kubernetes"
)

// ProviderFactory creates sandbox providers based on configuration
type ProviderFactory struct {
	dockerProvider func() (Provider, error)
	k8sProvider    func() (Provider, error)
}

// NewProviderFactory creates a new factory with provider constructors
func NewProviderFactory(dockerProvider, k8sProvider func() (Provider, error)) *ProviderFactory {
	return &ProviderFactory{
		dockerProvider: dockerProvider,
		k8sProvider:    k8sProvider,
	}
}

// Create creates a provider based on the SANDBOX_PROVIDER environment variable
// Returns Docker provider by default if not specified
func (f *ProviderFactory) Create() (Provider, error) {
	providerType := ProviderType(os.Getenv("SANDBOX_PROVIDER"))

	switch providerType {
	case ProviderTypeKubernetes:
		if f.k8sProvider == nil {
			return nil, fmt.Errorf("kubernetes provider not configured")
		}
		return f.k8sProvider()

	case ProviderTypeDocker, "":
		if f.dockerProvider == nil {
			return nil, fmt.Errorf("docker provider not configured")
		}
		return f.dockerProvider()

	default:
		return nil, fmt.Errorf("unknown sandbox provider: %s", providerType)
	}
}

// CreateWithType creates a provider of the specified type
func (f *ProviderFactory) CreateWithType(providerType ProviderType) (Provider, error) {
	switch providerType {
	case ProviderTypeKubernetes:
		if f.k8sProvider == nil {
			return nil, fmt.Errorf("kubernetes provider not configured")
		}
		return f.k8sProvider()

	case ProviderTypeDocker:
		if f.dockerProvider == nil {
			return nil, fmt.Errorf("docker provider not configured")
		}
		return f.dockerProvider()

	default:
		return nil, fmt.Errorf("unknown sandbox provider: %s", providerType)
	}
}

// DetectProviderType attempts to detect the best provider type based on environment
// Returns Docker if Docker socket is available, Kubernetes if running in a pod, or error
func DetectProviderType() ProviderType {
	// Check for explicit configuration first
	if pt := os.Getenv("SANDBOX_PROVIDER"); pt != "" {
		return ProviderType(pt)
	}

	// Check if running in Kubernetes (service account token exists)
	if _, err := os.Stat("/var/run/secrets/kubernetes.io/serviceaccount/token"); err == nil {
		return ProviderTypeKubernetes
	}

	// Default to Docker
	return ProviderTypeDocker
}
