package ipam

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

type Store struct {
	path      string
	cidr      *net.IPNet
	serverIP  string
	staticIPs map[string]string
	mu        sync.Mutex
	state     state
}

type state struct {
	Allocations map[string]string `json:"allocations"`
}

func Open(path, cidr, serverIP string, staticClients map[string]string) (*Store, error) {
	_, network, err := net.ParseCIDR(cidr)
	if err != nil {
		return nil, err
	}
	s := &Store{path: path, cidr: network, serverIP: serverIP, staticIPs: staticClients, state: state{Allocations: map[string]string{}}}
	if err := s.load(); err != nil {
		return nil, err
	}
	return s, nil
	}

func (s *Store) load() error {
	b, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	return json.Unmarshal(b, &s.state)
	}

func (s *Store) save() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s.state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, b, 0o644)
	}

func (s *Store) Allocate(nodeID string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ip := s.staticIPs[nodeID]; ip != "" {
		s.state.Allocations[nodeID] = ip
		return ip, s.save()
	}
	if ip := s.state.Allocations[nodeID]; ip != "" {
		return ip, nil
	}
	used := map[string]bool{s.serverIP: true}
	for _, ip := range s.state.Allocations {
		used[ip] = true
	}
	for _, ip := range s.staticIPs {
		used[ip] = true
	}
	for _, ip := range hosts(s.cidr) {
		if !used[ip] {
			s.state.Allocations[nodeID] = ip
			return ip, s.save()
		}
	}
	return "", fmt.Errorf("no available IPs in %s", s.cidr.String())
	}

func (s *Store) Snapshot() map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]string, len(s.state.Allocations))
	for k, v := range s.state.Allocations {
		out[k] = v
	}
	return out
	}

func hosts(network *net.IPNet) []string {
	start := network.IP.To4()
	if start == nil {
		return nil
	}
	var out []string
	for ip := incIP(start); network.Contains(ip); ip = incIP(ip) {
		candidate := append(net.IP(nil), ip...)
		out = append(out, candidate.String())
	}
	if len(out) > 0 {
		out = out[1:]
	}
	if len(out) > 0 {
		out = out[:len(out)-1]
	}
	sort.Strings(out)
	return out
	}

func incIP(ip net.IP) net.IP {
	next := append(net.IP(nil), ip.To4()...)
	for i := len(next) - 1; i >= 0; i-- {
		next[i]++
		if next[i] != 0 {
			break
		}
	}
	return next
	}
