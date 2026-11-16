package harness

import (
	"testing"

	"github.com/pihme/hermetarium/supervisor"
)

func CreateWeakEcho(t *testing.T, root, id string) (*supervisor.WeakInstance, error) {
	if err := EnsureEchoImage(root); err != nil {
		return nil, err
	}
	return supervisor.CreateWeak(root, id, echoOpts(root, supervisor.TrackTestContainers(t)))
}

func CreateStrongEcho(t *testing.T, root, id string) (*supervisor.StrongInstance, error) {
	if err := EnsureEchoImage(root); err != nil {
		return nil, err
	}
	return supervisor.CreateStrong(root, id, echoOpts(root, supervisor.TrackTestContainers(t)))
}

func CreateWeakAgentd(t *testing.T, root, id string, mock bool) (*supervisor.WeakInstance, error) {
	if err := EnsureAgentdImage(root); err != nil {
		return nil, err
	}
	opts, err := agentdOpts(root, supervisor.TrackTestContainers(t), mock)
	if err != nil {
		return nil, err
	}
	return supervisor.CreateWeak(root, id, opts)
}

func CreateStrongAgentd(t *testing.T, root, id string, mock bool) (*supervisor.StrongInstance, error) {
	if err := EnsureAgentdImage(root); err != nil {
		return nil, err
	}
	opts, err := agentdOpts(root, supervisor.TrackTestContainers(t), mock)
	if err != nil {
		return nil, err
	}
	return supervisor.CreateStrong(root, id, opts)
}

func CreateWeakInhabitant(t *testing.T, root, id, name string, mock bool) (*supervisor.WeakInstance, error) {
	opts, err := inhabitantOpts(root, name, supervisor.TrackTestContainers(t), mock)
	if err != nil {
		return nil, err
	}
	if err := EnsureOfficialImage(root, name, opts.Image); err != nil {
		return nil, err
	}
	return supervisor.CreateWeak(root, id, opts)
}
