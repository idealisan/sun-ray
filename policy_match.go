//go:build !nogui

package main

// policy_match.go answers the policy test window: it walks the rules in the
// order buildJSON writes them and says which one takes the query.

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strings"
	"sync"

	"github.com/xtls/xray-core/common/geodata"
)

// policyMatch is what the test window shows: which rule took the query and
// where it sends it.
type policyMatch struct {
	Source string // the rule that matched, as the user sees it
	Action string // direct, proxy or block
	Note   string // what the answer depends on, when it does
}

// matchPolicy answers the test window. The order is the one buildJSON
// writes: the user's policies, private addresses and local names, the ads
// list, the CN lists, and then the proxy.
func (a *app) matchPolicy(input string) (policyMatch, error) {
	target := strings.TrimSpace(input)
	if target == "" {
		return policyMatch{}, errors.New("请输入域名或 IP")
	}
	if addr, ok, err := parsePolicyAddress(target); err != nil {
		return policyMatch{}, err
	} else if ok {
		return a.matchPolicyIP(addr)
	}
	domain, err := normalizePolicyDomain(target, true)
	if err != nil {
		return policyMatch{}, err
	}
	return a.matchPolicyDomain(domain)
}

// parsePolicyAddress accepts an address, a CIDR (its first address is what
// is tested) and an address with a port. The second result is false for a
// domain.
func parsePolicyAddress(raw string) (netip.Addr, bool, error) {
	value := raw
	if strings.HasPrefix(value, "[") {
		if i := strings.Index(value, "]"); i > 0 {
			value = value[1:i]
		}
	} else if host, _, err := net.SplitHostPort(value); err == nil {
		value = host
	}
	if addr, err := netip.ParseAddr(value); err == nil {
		return addr.Unmap(), true, nil
	}
	if prefix, err := netip.ParsePrefix(value); err == nil {
		return prefix.Masked().Addr().Unmap(), true, nil
	}
	if strings.ContainsAny(raw, "/[]") {
		return netip.Addr{}, false, errors.New("不是有效的 IP 或网段")
	}
	return netip.Addr{}, false, nil
}

func (a *app) matchPolicyDomain(domain string) (policyMatch, error) {
	g := &a.cfg.Global
	for i, p := range g.Policies {
		p, err := normalizePolicy(p)
		if err != nil {
			continue
		}
		switch p.Type {
		case policyTypeDomain:
			if domain == p.Value || strings.HasSuffix(domain, "."+p.Value) {
				return policyMatch{Source: fmt.Sprintf("用户策略 #%d（%s %s）", i+1, policyTypeLabel(p.Type), p.Value), Action: p.Action}, nil
			}
		case policyTypeFull:
			if domain == p.Value {
				return policyMatch{Source: fmt.Sprintf("用户策略 #%d（%s %s）", i+1, policyTypeLabel(p.Type), p.Value), Action: p.Action}, nil
			}
		}
	}
	presets, err := loadPolicyPresets()
	if err != nil {
		return policyMatch{}, err
	}
	if presets.local.MatchAny(domain) {
		return policyMatch{Source: "内置规则（本地/局域网域名）", Action: policyActionDirect}, nil
	}
	if g.RouteAds && presets.ads.MatchAny(domain) {
		return policyMatch{Source: "内置规则（屏蔽广告）", Action: policyActionBlock}, nil
	}
	if g.RouteCN && presets.cnDomain.MatchAny(domain) {
		return policyMatch{Source: "内置规则（绕过大陆 geosite:cn）", Action: policyActionDirect}, nil
	}
	m := policyMatch{Source: "未命中直连规则", Action: policyActionProxy}
	switch {
	case g.DomainStrategy != dsIPIfNonMatch:
		m.Note = "当前不解析域名：未命中域名规则的连接交给代理解析"
	case g.TunEnabled:
		m.Note = "已开启解析域名：内核会用本地 DNS 解析后按 geoip:cn 判断，DNS 被污染时结论可能不同"
	default:
		m.Note = "普通模式下，未命中域名的连接会解析成 IP 后再按 geoip:cn 判断"
	}
	return m, nil
}

func (a *app) matchPolicyIP(addr netip.Addr) (policyMatch, error) {
	g := &a.cfg.Global
	for i, p := range g.Policies {
		p, err := normalizePolicy(p)
		if err != nil || p.Type != policyTypeIP {
			continue
		}
		if policyIPMatches(p.Value, addr) {
			return policyMatch{Source: fmt.Sprintf("用户策略 #%d（%s %s）", i+1, policyTypeLabel(p.Type), p.Value), Action: p.Action}, nil
		}
	}
	if privateAddress(addr) {
		return policyMatch{Source: "内置规则（私网/保留地址）", Action: policyActionDirect}, nil
	}
	presets, err := loadPolicyPresets()
	if err != nil {
		return policyMatch{}, err
	}
	if g.RouteCN && presets.cnIP.Match(net.IP(addr.AsSlice())) {
		return policyMatch{Source: "内置规则（绕过大陆 geoip:cn）", Action: policyActionDirect}, nil
	}
	return policyMatch{Source: "未命中直连规则", Action: policyActionProxy}, nil
}

func policyIPMatches(value string, addr netip.Addr) bool {
	if strings.Contains(value, "/") {
		prefix, err := netip.ParsePrefix(value)
		return err == nil && prefix.Contains(addr)
	}
	other, err := netip.ParseAddr(value)
	return err == nil && other.Unmap() == addr
}

// privatePrefixes is privateSpecialCIDRs parsed once.
var privatePrefixes = func() []netip.Prefix {
	prefixes := make([]netip.Prefix, 0, len(privateSpecialCIDRs))
	for _, value := range privateSpecialCIDRs {
		if prefix, err := netip.ParsePrefix(value); err == nil {
			prefixes = append(prefixes, prefix)
		}
	}
	return prefixes
}()

func privateAddress(addr netip.Addr) bool {
	for _, prefix := range privatePrefixes {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}

// policyPresets holds the matchers built from the embedded geo data, made
// once on the first test.
type policyPresets struct {
	once     sync.Once
	local    geodata.DomainMatcher
	ads      geodata.DomainMatcher
	cnDomain geodata.DomainMatcher
	cnIP     geodata.IPMatcher
	err      error
}

var presets policyPresets

func loadPolicyPresets() (*policyPresets, error) {
	presets.once.Do(func() {
		domainMatcher := func(values []string) (geodata.DomainMatcher, error) {
			rules, err := geodata.ParseDomainRules(values, geodata.Domain_Domain)
			if err != nil {
				return nil, err
			}
			return geodata.DomainReg.BuildDomainMatcher(rules)
		}
		if presets.local, presets.err = domainMatcher(localDomainRules); presets.err != nil {
			return
		}
		if presets.ads, presets.err = domainMatcher([]string{"geosite:category-ads-all"}); presets.err != nil {
			return
		}
		if presets.cnDomain, presets.err = domainMatcher([]string{"geosite:cn"}); presets.err != nil {
			return
		}
		rules, err := geodata.ParseIPRules([]string{"geoip:cn"})
		if err != nil {
			presets.err = err
			return
		}
		presets.cnIP, presets.err = geodata.IPReg.BuildIPMatcher(rules)
	})
	return &presets, presets.err
}
