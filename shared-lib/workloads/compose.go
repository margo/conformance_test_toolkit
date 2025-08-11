package workloads

// DockerComposeClient interface
type DockerComposeClient struct {
}

func NewDockerComposeClient() (*DockerComposeClient, error) {
	return &DockerComposeClient{}, nil
}
