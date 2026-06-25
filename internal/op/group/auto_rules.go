package group

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/gypg/lodestar/internal/utils/log"
)

// familyRuleJSON is the JSON-serialisable form of a single auto-group family rule.
type familyRuleJSON struct {
	Canonical string `json:"canonical"`
	RuleName  string `json:"rule_name"`
	Pattern   string `json:"pattern"`
}

// autoGroupRulesConfig is the top-level structure of auto_group_rules.json.
type autoGroupRulesConfig struct {
	Rules []familyRuleJSON `json:"rules"`
}

var (
	rulesOnce     sync.Once
	loadedFromCfg bool
)

// loadFamilyRulesFromFile reads data/auto_group_rules.json and returns the parsed
// rules. If the file does not exist it returns (nil, false) so callers can fall
// back to built-in defaults.
func loadFamilyRulesFromFile() ([]autoGroupFamilyRule, bool) {
	cfgPath := filepath.Join("data", "auto_group_rules.json")

	// Also honour LODGE_DATA_DIR / LODGE_DATA_DIR env var if set (mirrors conf.defaultDataDir).
	if envDir := os.Getenv("LODESTAR_DATA_DIR"); strings.TrimSpace(envDir) != "" {
		cfgPath = filepath.Join(strings.TrimSpace(envDir), "auto_group_rules.json")
	}

	data, err := os.ReadFile(cfgPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false
		}
		log.Warnf("auto group rules: failed to read %s: %v", cfgPath, err)
		return nil, false
	}

	var cfg autoGroupRulesConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		log.Warnf("auto group rules: failed to parse %s: %v (using built-in defaults)", cfgPath, err)
		return nil, false
	}

	rules := make([]autoGroupFamilyRule, 0, len(cfg.Rules))
	for i, r := range cfg.Rules {
		if r.Canonical == "" || r.Pattern == "" {
			log.Warnf("auto group rules: skipping rule #%d: canonical and pattern are required", i+1)
			continue
		}
		re, err := regexp.Compile(r.Pattern)
		if err != nil {
			log.Warnf("auto group rules: skipping rule #%d (%s): invalid regex %q: %v", i+1, r.Canonical, r.Pattern, err)
			continue
		}
		name := r.RuleName
		if name == "" {
			name = r.Canonical
		}
		rules = append(rules, autoGroupFamilyRule{
			canonical: r.Canonical,
			ruleName:  name,
			re:        re,
		})
	}

	if len(rules) == 0 {
		log.Warnf("auto group rules: %s contained no valid rules (using built-in defaults)", cfgPath)
		return nil, false
	}

	log.Infof("auto group rules: loaded %d rules from %s", len(rules), cfgPath)
	return rules, true
}

// initAutoGroupFamilyRules replaces the package-level autoGroupFamilyRules slice
// with the config-loaded version, or keeps the built-in defaults if no config
// file is found.  Safe to call multiple times (idempotent).
func initAutoGroupFamilyRules() {
	rulesOnce.Do(func() {
		if rules, ok := loadFamilyRulesFromFile(); ok {
			autoGroupFamilyRules = rules
			loadedFromCfg = true
		}
		// else: keep the hardcoded defaults already in autoGroupFamilyRules
	})
}

// RulesLoadedFromConfig returns true when rules were loaded from the JSON file
// rather than using built-in defaults. Exposed for testing / diagnostics.
func RulesLoadedFromConfig() bool {
	initAutoGroupFamilyRules()
	return loadedFromCfg
}

// ExportDefaultRules writes the current built-in default rules to the given
// path as JSON. Useful to bootstrap the config file for customisation.
func ExportDefaultRules(path string) error {
	cfg := autoGroupRulesConfig{
		Rules: make([]familyRuleJSON, 0, len(defaultFamilyRules)),
	}
	for _, r := range defaultFamilyRules {
		cfg.Rules = append(cfg.Rules, familyRuleJSON{
			Canonical: r.canonical,
			RuleName:  r.ruleName,
			Pattern:   r.re.String(),
		})
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}
