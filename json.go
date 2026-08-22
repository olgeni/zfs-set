package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/olgeni/zfs-set/props"
)

type jsonValue struct {
	Name     string `json:"name"`
	Value    string `json:"value"`
	Raw      string `json:"raw,omitempty"`
	Source   string `json:"source"`
	From     string `json:"inherited_from,omitempty"`
	Received string `json:"received,omitempty"`
	Kind     string `json:"kind,omitempty"`
	Group    string `json:"group,omitempty"`
	Note     string `json:"note,omitempty"`
}

type jsonListingDoc struct {
	Dataset    string      `json:"dataset"`
	Type       string      `json:"type"`
	Mountpoint string      `json:"mountpoint,omitempty"`
	Mounted    bool        `json:"mounted"`
	Properties []jsonValue `json:"properties"`
}

func jsonListing(l *props.Listing, localOnly bool) jsonListingDoc {
	d := jsonListingDoc{Dataset: l.Dataset.Name, Type: l.Dataset.Type, Mountpoint: l.Dataset.Mountpoint, Mounted: l.Dataset.Mounted, Properties: []jsonValue{}}
	for _, v := range l.Props {
		if localOnly && !v.Local() {
			continue
		}
		j := jsonValue{Name: v.Name, Value: v.Value, Source: v.Source, From: v.From, Received: v.Received}
		if v.Raw != v.Value {
			j.Raw = v.Raw
		}
		if p, ok := props.Lookup(v.Name); ok {
			j.Kind, j.Group, j.Note = p.Kind.String(), p.Group, p.Note
		}
		d.Properties = append(d.Properties, j)
	}
	return d
}

type jsonStep struct {
	Args    []string `json:"args"`
	Command string   `json:"command"`
	Desc    string   `json:"description"`
}

type jsonNote struct {
	Fatal bool   `json:"fatal"`
	Msg   string `json:"message"`
}

type jsonPlanDoc struct {
	Dataset string     `json:"dataset,omitempty"`
	Steps   []jsonStep `json:"commands"`
	Notes   []jsonNote `json:"notes"`
}

func jsonPlan(p *props.Plan, probs []props.Problem) jsonPlanDoc {
	d := jsonPlanDoc{Dataset: p.Dataset, Steps: []jsonStep{}, Notes: []jsonNote{}}
	for _, s := range p.Steps {
		d.Steps = append(d.Steps, jsonStep{Args: s.Args, Command: s.String(), Desc: s.Desc})
	}
	for _, pr := range probs {
		d.Notes = append(d.Notes, jsonNote{pr.Fatal, pr.Msg})
	}
	return d
}

type jsonSpot struct {
	Dataset string `json:"dataset"`
	Value   string `json:"value"`
	Source  string `json:"source"`
}

type jsonWhereDoc struct {
	Property string     `json:"property"`
	Datasets []jsonSpot `json:"datasets"`
}

func jsonWhere(prop string, hits []props.Spot) jsonWhereDoc {
	d := jsonWhereDoc{Property: prop, Datasets: []jsonSpot{}}
	for _, s := range hits {
		d.Datasets = append(d.Datasets, jsonSpot{s.Dataset, s.Value, s.Source})
	}
	return d
}

type jsonTreeDoc struct {
	Property    string     `json:"property"`
	Dataset     string     `json:"dataset"`
	Chain       []jsonSpot `json:"chain"`
	Descendants []jsonSpot `json:"overriding_descendants"`
}

func jsonTree(prop, ds string, chain, below []props.Spot) jsonTreeDoc {
	d := jsonTreeDoc{Property: prop, Dataset: ds, Chain: []jsonSpot{}, Descendants: []jsonSpot{}}
	for _, s := range chain {
		d.Chain = append(d.Chain, jsonSpot{s.Dataset, s.Value, s.Source})
	}
	for _, s := range below {
		d.Descendants = append(d.Descendants, jsonSpot{s.Dataset, s.Value, s.Source})
	}
	return d
}

type jsonOption struct {
	Value string `json:"value"`
	Note  string `json:"note,omitempty"`
}

type jsonPropDoc struct {
	Name    string       `json:"name"`
	Alias   string       `json:"alias,omitempty"`
	Kind    string       `json:"kind"`
	Inherit bool         `json:"inherited"`
	Default string       `json:"default,omitempty"`
	Group   string       `json:"group"`
	Types   string       `json:"applies_to"`
	Note    string       `json:"note"`
	Detail  string       `json:"detail,omitempty"`
	Feature string       `json:"pool_feature,omitempty"`
	Linux   bool         `json:"no_effect_on_freebsd,omitempty"`
	NewData bool         `json:"new_data_only,omitempty"`
	Family  bool         `json:"per_key,omitempty"`
	Values  []jsonOption `json:"values,omitempty"`
	Hint    string       `json:"syntax,omitempty"`
}

func jsonProp(p *props.Prop) jsonPropDoc {
	d := jsonPropDoc{Name: p.Name, Alias: p.Short, Kind: p.Kind.String(), Inherit: p.Inherit, Default: p.Default, Group: p.Group, Types: p.TypesLabel(), Note: p.Note, Detail: p.Detail, Feature: p.Feature, Linux: p.Linux, NewData: p.NewData, Family: p.Family, Hint: p.Hint()}
	for _, o := range p.Choices() {
		d.Values = append(d.Values, jsonOption{o.Value, o.Note})
	}
	return d
}

func jsonCatalogue() []jsonPropDoc {
	var res []jsonPropDoc
	for i := range props.Catalogue {
		res = append(res, jsonProp(&props.Catalogue[i]))
	}
	return res
}

func printJSON(v any) int {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		fmt.Fprintln(os.Stderr, "zfs-set:", err)
		return 1
	}
	return 0
}
