package harness

import (
	"testing"

	"github.com/pihme/hermetarium/supervisor"
)

func CreateWeakEcho(t *testing.T, root, id string) (*supervisor.WeakInstance, error) {
	if err := EnsureEchoImage(root); err != nil {
		return nil, err
	}
	label := supervisor.TrackTestContainers(t)
	registerSidecars(root, id, label, true, false)
	return supervisor.CreateWeak(root, id, echoOpts(root, label))
}

func CreateStrongEcho(t *testing.T, root, id string) (*supervisor.StrongInstance, error) {
	if err := EnsureEchoImage(root); err != nil {
		return nil, err
	}
	label := supervisor.TrackTestContainers(t)
	registerSidecars(root, id, label, true, false)
	return supervisor.CreateStrong(root, id, echoOpts(root, label))
}

func CreateWeakAgentd(t *testing.T, root, id string, mock bool) (*supervisor.WeakInstance, error) {
	if err := EnsureAgentdImage(root); err != nil {
		return nil, err
	}
	label := supervisor.TrackTestContainers(t)
	opts, err := agentdOpts(root, label, mock)
	if err != nil {
		return nil, err
	}
	registerSidecars(root, id, label, false, mock)
	return supervisor.CreateWeak(root, id, opts)
}

func CreateStrongAgentd(t *testing.T, root, id string, mock bool) (*supervisor.StrongInstance, error) {
	if err := EnsureAgentdImage(root); err != nil {
		return nil, err
	}
	label := supervisor.TrackTestContainers(t)
	opts, err := agentdOpts(root, label, mock)
	if err != nil {
		return nil, err
	}
	registerSidecars(root, id, label, false, mock)
	return supervisor.CreateStrong(root, id, opts)
}

func CreateWeakInhabitant(t *testing.T, root, id, name string, mock bool) (*supervisor.WeakInstance, error) {
	label := supervisor.TrackTestContainers(t)
	opts, err := inhabitantOpts(root, name, label, mock)
	if err != nil {
		return nil, err
	}
	if err := EnsureOfficialImage(root, name, opts.Image); err != nil {
		return nil, err
	}
	registerSidecars(root, id, label, false, mock)
	return supervisor.CreateWeak(root, id, opts)
}
