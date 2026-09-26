//go:build !linux

package main

// sampler hors Linux : inventaire seulement (pas de métriques).
type sampler struct {
	root string
}

func (s *sampler) Sample() ([]point, error) { return nil, ErrUnsupported }

func (s *sampler) Host() (HostInfo, error) { return HostInfo{}, nil }
