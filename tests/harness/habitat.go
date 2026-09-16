package harness

import "github.com/pihme/hermetarium/supervisor"

func CreateWeakEcho(root, id string) (*supervisor.WeakInstance, error) {
	if err := EnsureEchoImage(root); err != nil {
		return nil, err
	}
	return supervisor.CreateWeak(root, id, echoOpts(root))
}

func CreateStrongEcho(root, id string) (*supervisor.StrongInstance, error) {
	if err := EnsureEchoImage(root); err != nil {
		return nil, err
	}
	return supervisor.CreateStrong(root, id, echoOpts(root))
}

func CreateWeakAgentd(root, id string, mock bool) (*supervisor.WeakInstance, error) {
	if err := EnsureAgentdImage(root); err != nil {
		return nil, err
	}
	opts, err := agentdOpts(root, mock)
	if err != nil {
		return nil, err
	}
	return supervisor.CreateWeak(root, id, opts)
}

func CreateStrongAgentd(root, id string, mock bool) (*supervisor.StrongInstance, error) {
	if err := EnsureAgentdImage(root); err != nil {
		return nil, err
	}
	opts, err := agentdOpts(root, mock)
	if err != nil {
		return nil, err
	}
	return supervisor.CreateStrong(root, id, opts)
}

func CreateWeakInhabitant(root, id, name string, mock bool) (*supervisor.WeakInstance, error) {
	opts, err := inhabitantOpts(root, name, mock)
	if err != nil {
		return nil, err
	}
	if err := EnsureOfficialImage(root, name, opts.Image); err != nil {
		return nil, err
	}
	return supervisor.CreateWeak(root, id, opts)
}
