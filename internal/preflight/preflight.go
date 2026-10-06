// Package preflight checks, before anything is deployed, that the subscription can run the
// profile in the chosen region: every VM size is offered to it there (Free Trial and new
// subscriptions often aren't), in the zones the cluster uses, with enough vCPU quota; and the
// PostgreSQL SKU exists there. Each of these otherwise fails deep into `bluepave up`.
package preflight

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/kushan94/bluepave/internal/run"
)

// Zones are the availability zones the aks module spreads node pools across.
var Zones = []string{"1", "2", "3"}

// Pool is one AKS node pool of the profile.
type Pool struct {
	Name     string `yaml:"name"`
	VMSize   string `yaml:"vmSize"`
	Spot     bool   `yaml:"spot"`
	Count    int    `yaml:"count"`
	MinCount int    `yaml:"minCount"`
	MaxCount int    `yaml:"maxCount"`
}

// Profile is what preflight reads from profiles/<name>.yaml.
type Profile struct {
	Spec struct {
		Cluster struct {
			SystemPool Pool   `yaml:"systemPool"`
			UserPools  []Pool `yaml:"userPools"`
		} `yaml:"cluster"`
		Data struct {
			Postgres struct {
				SkuName string `yaml:"skuName"`
			} `yaml:"postgres"`
		} `yaml:"data"`
	} `yaml:"spec"`
}

// LoadProfile reads profiles/<name>.yaml under root.
func LoadProfile(root, name string) (*Profile, error) {
	data, err := os.ReadFile(filepath.Join(root, "profiles", name+".yaml"))
	if err != nil {
		return nil, err
	}
	var p Profile
	if err := yaml.Unmarshal(data, &p); err != nil {
		return nil, err
	}
	p.Spec.Cluster.SystemPool.Name = "system"
	return &p, nil
}

// Input is what to check.
type Input struct {
	SubscriptionID string
	Region         string
	Profile        *Profile
	// Clusters is how many clusters will be created in the region (environments without a cluster
	// yet). Quota is only checked for those: an existing cluster already holds its cores.
	Clusters int
	// Postgres: whether the data-postgres module is on.
	Postgres bool
}

// Result is what preflight found: Problems stop `up`; Notes are informational.
type Result struct {
	Problems []string
	Notes    []string
}

// Check runs the checks with az (signed in).
func Check(ctx context.Context, r run.Runner, in Input) (*Result, error) {
	res := &Result{}
	skus, err := vmSkus(ctx, r, in.SubscriptionID, in.Region)
	if err != nil {
		return nil, err
	}
	pools := append([]Pool{in.Profile.Spec.Cluster.SystemPool}, in.Profile.Spec.Cluster.UserPools...)

	// Sizes: offered in the region, in every zone the cluster uses.
	need := map[string]int{} // quota name -> vCPUs needed
	for _, p := range pools {
		s, ok := skus[strings.ToLower(p.VMSize)]
		if !ok {
			res.Problems = append(res.Problems, fmt.Sprintf("pool %s: VM size %s doesn't exist in %s", p.Name, p.VMSize, in.Region))
			continue
		}
		if s.locationRestricted {
			res.Problems = append(res.Problems, fmt.Sprintf("pool %s: VM size %s isn't available to this subscription in %s (NotAvailableForSubscription); choose another region, or ask Azure support to enable it", p.Name, p.VMSize, in.Region))
			continue
		}
		if missing := missingZones(s); len(missing) > 0 {
			res.Problems = append(res.Problems, fmt.Sprintf("pool %s: VM size %s isn't available in zone(s) %s of %s for this subscription; the cluster uses zones %s", p.Name, p.VMSize, strings.Join(missing, ", "), in.Region, strings.Join(Zones, ", ")))
		}
		nodes := max(p.MaxCount, p.Count, p.MinCount)
		if p.Spot {
			need["lowPriorityCores"] += nodes * s.vcpus
		} else {
			need[s.family] += nodes * s.vcpus
			need["cores"] += nodes * s.vcpus
		}
	}

	// Quota: what new clusters need against what's free.
	if in.Clusters > 0 && len(res.Problems) == 0 {
		usage, err := usages(ctx, r, in.Region)
		if err != nil {
			return nil, err
		}
		names := make([]string, 0, len(need))
		for n := range need {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			want := need[n] * in.Clusters
			u, ok := usage[n]
			if !ok {
				continue
			}
			if free := u.limit - u.used; want > free {
				res.Problems = append(res.Problems, fmt.Sprintf("quota %s in %s: the cluster needs %d vCPUs, %d of %d are free (other VMs in the region use the rest); free them, choose another region, or request more quota", u.label, in.Region, want, free, u.limit))
			} else {
				res.Notes = append(res.Notes, fmt.Sprintf("quota %s in %s: %d of %d free, %d needed", u.label, in.Region, free, u.limit, want))
			}
		}
	}

	if in.Postgres {
		sku := in.Profile.Spec.Data.Postgres.SkuName
		ok, err := postgresSku(ctx, r, in.SubscriptionID, in.Region, sku)
		if err != nil {
			return nil, err
		}
		if !ok {
			res.Problems = append(res.Problems, fmt.Sprintf("PostgreSQL Flexible Server SKU %s isn't offered to this subscription in %s", sku, in.Region))
		}
	}
	return res, nil
}

type vmSku struct {
	family             string
	vcpus              int
	zones              []string
	locationRestricted bool
	restrictedZones    []string
}

func missingZones(s vmSku) []string {
	var missing []string
	for _, z := range Zones {
		if !slices.Contains(s.zones, z) || slices.Contains(s.restrictedZones, z) {
			missing = append(missing, z)
		}
	}
	return missing
}

// vmSkus lists the region's VM sizes with this subscription's restrictions.
func vmSkus(ctx context.Context, r run.Runner, sub, region string) (map[string]vmSku, error) {
	url := fmt.Sprintf("https://management.azure.com/subscriptions/%s/providers/Microsoft.Compute/skus?api-version=2021-07-01&$filter=location eq '%s'", sub, region)
	out, err := r.Run(ctx, "az", "rest", "--method", "get", "--url", url, "--output", "json")
	if err != nil {
		return nil, fmt.Errorf("list VM sizes in %s: %w", region, err)
	}
	var doc struct {
		Value []struct {
			ResourceType string `json:"resourceType"`
			Name         string `json:"name"`
			Family       string `json:"family"`
			LocationInfo []struct {
				Zones []string `json:"zones"`
			} `json:"locationInfo"`
			Capabilities []struct {
				Name  string `json:"name"`
				Value string `json:"value"`
			} `json:"capabilities"`
			Restrictions []struct {
				Type            string `json:"type"`
				RestrictionInfo struct {
					Zones []string `json:"zones"`
				} `json:"restrictionInfo"`
			} `json:"restrictions"`
		} `json:"value"`
	}
	if err := json.Unmarshal(out, &doc); err != nil {
		return nil, fmt.Errorf("list VM sizes in %s: %w", region, err)
	}
	skus := map[string]vmSku{}
	for _, v := range doc.Value {
		if v.ResourceType != "virtualMachines" {
			continue
		}
		s := vmSku{family: v.Family}
		for _, c := range v.Capabilities {
			if c.Name == "vCPUs" {
				s.vcpus, _ = strconv.Atoi(c.Value)
			}
		}
		if len(v.LocationInfo) > 0 {
			s.zones = v.LocationInfo[0].Zones
		}
		for _, rs := range v.Restrictions {
			switch rs.Type {
			case "Location":
				s.locationRestricted = true
			case "Zone":
				s.restrictedZones = append(s.restrictedZones, rs.RestrictionInfo.Zones...)
			}
		}
		skus[strings.ToLower(v.Name)] = s
	}
	return skus, nil
}

type usage struct {
	label       string
	used, limit int
}

// usages is the region's compute quota by name (cores, <family>, lowPriorityCores).
func usages(ctx context.Context, r run.Runner, region string) (map[string]usage, error) {
	out, err := r.Run(ctx, "az", "vm", "list-usage", "--location", region, "--output", "json")
	if err != nil {
		return nil, fmt.Errorf("compute quota in %s: %w", region, err)
	}
	var list []struct {
		Name struct {
			Value          string `json:"value"`
			LocalizedValue string `json:"localizedValue"`
		} `json:"name"`
		CurrentValue json.Number `json:"currentValue"`
		Limit        json.Number `json:"limit"`
	}
	if err := json.Unmarshal(out, &list); err != nil {
		return nil, fmt.Errorf("compute quota in %s: %w", region, err)
	}
	m := map[string]usage{}
	for _, u := range list {
		used, _ := u.CurrentValue.Int64()
		limit, _ := u.Limit.Int64()
		m[u.Name.Value] = usage{label: u.Name.LocalizedValue, used: int(used), limit: int(limit)}
	}
	return m, nil
}

// postgresSku reports whether the region offers the Flexible Server SKU to this subscription.
func postgresSku(ctx context.Context, r run.Runner, sub, region, sku string) (bool, error) {
	url := fmt.Sprintf("https://management.azure.com/subscriptions/%s/providers/Microsoft.DBforPostgreSQL/locations/%s/capabilities?api-version=2024-08-01", sub, region)
	out, err := r.Run(ctx, "az", "rest", "--method", "get", "--url", url, "--output", "json")
	if err != nil {
		return false, fmt.Errorf("PostgreSQL capabilities in %s: %w", region, err)
	}
	var doc struct {
		Value []struct {
			// "Enabled" when the subscription can't create servers in the region at all.
			Restricted string `json:"restricted"`
			Editions   []struct {
				Skus []struct {
					Name string `json:"name"`
				} `json:"supportedServerSkus"`
			} `json:"supportedServerEditions"`
		} `json:"value"`
	}
	if err := json.Unmarshal(out, &doc); err != nil {
		return false, fmt.Errorf("PostgreSQL capabilities in %s: %w", region, err)
	}
	for _, c := range doc.Value {
		if strings.EqualFold(c.Restricted, "Enabled") {
			continue
		}
		for _, e := range c.Editions {
			for _, s := range e.Skus {
				if strings.EqualFold(s.Name, sku) {
					return true, nil
				}
			}
		}
	}
	return false, nil
}
