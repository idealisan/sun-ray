package main

// policy.go holds the user's own routing rules: how they are written down,
// checked, rendered into xray rules and matched when the app answers the
// test window. A policy sits before every preset, so it can send a domain
// or an address the other way.

import (
	"errors"
	"net/netip"
	"strings"
)

const (
	policyTypeDomain = "domain" // the domain and its subdomains
	policyTypeFull   = "full"   // exactly this domain
	policyTypeIP     = "ip"     // an IP address or a CIDR

	policyActionDirect = "direct"
	policyActionProxy  = "proxy"
	policyActionBlock  = "block"
)

var (
	policyTypeValues   = []string{policyTypeDomain, policyTypeFull, policyTypeIP}
	policyTypeLabels   = []string{"域名（含子域）", "域名（精确）", "IP·CIDR"}
	policyActionValues = []string{policyActionDirect, policyActionProxy, policyActionBlock}
	policyActionLabels = []string{"直连", "代理", "拦截"}
)

func policyTypeLabel(value string) string {
	for i, v := range policyTypeValues {
		if v == value {
			return policyTypeLabels[i]
		}
	}
	return policyTypeLabels[0]
}

func policyTypeValue(label string) string {
	for i, l := range policyTypeLabels {
		if l == label {
			return policyTypeValues[i]
		}
	}
	return policyTypeValues[0]
}

func policyActionLabel(value string) string {
	for i, v := range policyActionValues {
		if v == value {
			return policyActionLabels[i]
		}
	}
	return policyActionLabels[0]
}

func policyActionValue(label string) string {
	for i, l := range policyActionLabels {
		if l == label {
			return policyActionValues[i]
		}
	}
	return policyActionValues[0]
}

// normalizePolicy checks a rule and returns it as it is stored and matched.
// The message is what the table shows under the value.
func normalizePolicy(p policyConfig) (policyConfig, error) {
	p.Value = strings.TrimSpace(p.Value)
	switch p.Type {
	case policyTypeIP:
		value, err := normalizePolicyIP(p.Value)
		if err != nil {
			return p, err
		}
		p.Value = value
	case policyTypeDomain, policyTypeFull:
		value, err := normalizePolicyDomain(p.Value, p.Type == policyTypeDomain)
		if err != nil {
			return p, err
		}
		p.Value = value
	default:
		return p, errors.New("未知的策略类型")
	}
	switch p.Action {
	case policyActionDirect, policyActionProxy, policyActionBlock:
	default:
		return p, errors.New("未知的策略动作")
	}
	return p, nil
}

func normalizePolicyIP(value string) (string, error) {
	if strings.Contains(value, "/") {
		prefix, err := netip.ParsePrefix(value)
		if err != nil {
			return "", errors.New("不是有效的网段，例如 10.0.0.0/8")
		}
		return prefix.Masked().String(), nil
	}
	addr, err := netip.ParseAddr(value)
	if err != nil {
		return "", errors.New("不是有效的 IP 地址，例如 1.2.3.4 或 10.0.0.0/8")
	}
	return addr.Unmap().String(), nil
}

func normalizePolicyDomain(value string, suffix bool) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.TrimSuffix(value, ".")
	if suffix {
		value = strings.TrimPrefix(value, "*.")
	} else if strings.Contains(value, "*") {
		return "", errors.New("精确匹配不支持通配符")
	}
	if value == "" {
		return "", errors.New("请填写域名")
	}
	if strings.ContainsAny(value, " *:/@\\") {
		return "", errors.New("域名不能带通配符、端口或路径")
	}
	if numericHost(value) {
		return "", errors.New("不是有效的 IP 地址")
	}
	return value, nil
}

// numericHost reports whether value is only digits and dots, which is a
// mistyped address rather than a domain.
func numericHost(value string) bool {
	for _, r := range value {
		if r != '.' && (r < '0' || r > '9') {
			return false
		}
	}
	return value != ""
}

// policyRule renders one policy as an xray routing rule.
func policyRule(p policyConfig) map[string]any {
	if p.Type == policyTypeIP {
		return map[string]any{"type": "field", "ip": []string{p.Value}, "outboundTag": p.Action}
	}
	value := "domain:" + p.Value
	if p.Type == policyTypeFull {
		value = "full:" + p.Value
	}
	return map[string]any{"type": "field", "domain": []string{value}, "outboundTag": p.Action}
}

func hasBlockPolicy(policies []policyConfig) bool {
	for _, p := range policies {
		if p.Action == policyActionBlock {
			return true
		}
	}
	return false
}
