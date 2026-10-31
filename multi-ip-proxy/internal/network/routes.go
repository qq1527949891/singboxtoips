package network

import (
	"context"
	"encoding/json"
	"fmt"
	"net/netip"
	"strconv"
	"strings"

	"mipproxy.local/mipproxy/internal/model"
)

type Command struct {
	Args []string `json:"args"`
}

func RouteCommands(n model.Node, up bool) []Command {
	if !n.PolicyRouting {
		return nil
	}
	table, mark := strconv.Itoa(n.Table()), strconv.Itoa(n.Mark())
	if up {
		return []Command{
			{[]string{"ip", "-4", "route", "replace", n.Prefix, "dev", n.Interface, "scope", "link", "src", n.LocalIP, "table", table, "proto", "static"}},
			{[]string{"ip", "-4", "route", "replace", n.Gateway + "/32", "dev", n.Interface, "scope", "link", "src", n.LocalIP, "table", table, "proto", "static"}},
			{[]string{"ip", "-4", "route", "replace", "default", "via", n.Gateway, "dev", n.Interface, "src", n.LocalIP, "table", table, "proto", "static"}},
			{[]string{"ip", "-4", "rule", "add", "priority", strconv.Itoa(n.SourcePriority()), "from", n.LocalIP + "/32", "lookup", table}},
			{[]string{"ip", "-4", "rule", "add", "priority", strconv.Itoa(n.MarkPriority()), "fwmark", mark + "/0xffffffff", "lookup", table}},
		}
	}
	return []Command{
		{[]string{"ip", "-4", "rule", "del", "priority", strconv.Itoa(n.MarkPriority()), "fwmark", mark + "/0xffffffff", "lookup", table}},
		{[]string{"ip", "-4", "rule", "del", "priority", strconv.Itoa(n.SourcePriority()), "from", n.LocalIP + "/32", "lookup", table}},
		{[]string{"ip", "-4", "route", "del", "default", "via", n.Gateway, "dev", n.Interface, "src", n.LocalIP, "table", table, "proto", "static"}},
		{[]string{"ip", "-4", "route", "del", n.Gateway + "/32", "dev", n.Interface, "scope", "link", "src", n.LocalIP, "table", table, "proto", "static"}},
		{[]string{"ip", "-4", "route", "del", n.Prefix, "dev", n.Interface, "scope", "link", "src", n.LocalIP, "table", table, "proto", "static"}},
	}
}

func ruleMatches(row map[string]any, n model.Node, mark bool) bool {
	for key, value := range row {
		switch key {
		case "priority", "table", "src", "fwmark", "fwmask", "protocol":
		case "dst":
			if value != "all" && value != "0.0.0.0/0" {
				return false
			}
		case "tos":
			if intValue(value) != 0 {
				return false
			}
		case "flags":
			if flags, ok := value.([]any); !ok || len(flags) != 0 {
				return false
			}
		default:
			return false
		}
	}
	if intValue(row["table"]) != n.Table() {
		return false
	}
	if mark {
		if row["fwmask"] != nil && intValue(row["fwmask"]) != 0xffffffff {
			return false
		}
		return intValue(row["priority"]) == n.MarkPriority() && intValue(row["fwmark"]) == n.Mark() && (row["src"] == nil || row["src"] == "all")
	}
	src, _ := row["src"].(string)
	return intValue(row["priority"]) == n.SourcePriority() && strings.TrimSuffix(src, "/32") == n.LocalIP && row["fwmark"] == nil
}

func readRules(ctx context.Context, r Runner) ([]map[string]any, error) {
	b, err := r.Run(ctx, "ip", "-j", "-4", "rule", "show")
	if err != nil {
		return nil, err
	}
	var rows []map[string]any
	err = json.Unmarshal(b, &rows)
	return rows, err
}

func readTable(ctx context.Context, r Runner, table int) ([]map[string]any, error) {
	b, err := r.Run(ctx, "ip", "-j", "-4", "route", "show", "table", strconv.Itoa(table))
	if err != nil {
		if strings.Contains(string(b), "FIB table does not exist") {
			return nil, nil
		}
		return nil, err
	}
	var rows []map[string]any
	err = json.Unmarshal(b, &rows)
	return rows, err
}

func managedDestination(dst string, n model.Node) bool {
	return dst == "default" || dst == n.Prefix || dst == n.Gateway || dst == n.Gateway+"/32"
}

func routeMatches(row map[string]any, n model.Node) bool {
	dst, _ := row["dst"].(string)
	if !managedDestination(dst, n) || row["dev"] != n.Interface || row["prefsrc"] != n.LocalIP {
		return false
	}
	if row["protocol"] != "static" && intValue(row["protocol"]) != 4 {
		return false
	}
	for key, value := range row {
		switch key {
		case "dst", "dev", "prefsrc", "protocol", "gateway", "scope", "table":
		case "flags":
			if flags, ok := value.([]any); !ok || len(flags) != 0 {
				return false
			}
		default:
			return false
		}
	}
	if dst == "default" {
		return row["gateway"] == n.Gateway && (row["scope"] == nil || row["scope"] == "global")
	}
	return row["gateway"] == nil && row["scope"] == "link"
}

func CheckConflicts(ctx context.Context, r Runner, s model.State, old *model.State) error {
	rows, err := readRules(ctx, r)
	if err != nil {
		return err
	}
	for _, n := range s.Nodes {
		if !n.PolicyRouting {
			continue
		}
		for _, row := range rows {
			priority := intValue(row["priority"])
			if priority != n.SourcePriority() && priority != n.MarkPriority() {
				continue
			}
			owned := false
			if old != nil {
				for _, o := range old.Nodes {
					if o.PolicyRouting && (ruleMatches(row, o, false) || ruleMatches(row, o, true)) {
						owned = true
					}
				}
			}
			if !owned {
				return fmt.Errorf("·�ɹ������ȼ� %d �ѱ���������ռ�ã�δ�޸�", priority)
			}
		}
		var ownedTable *model.Node
		if old != nil {
			for _, o := range old.Nodes {
				if o.PolicyRouting && o.Table() == n.Table() {
					copy := o
					ownedTable = &copy
				}
			}
		}
		routes, err := readTable(ctx, r, n.Table())
		if err != nil {
			return err
		}
		if ownedTable == nil && len(routes) > 0 {
			return fmt.Errorf("·�ɱ� %d �ѱ�ռ�ã�δ�޸�", n.Table())
		}
		for _, row := range routes {
			dst, _ := row["dst"].(string)
			if ownedTable != nil && (managedDestination(dst, n) || managedDestination(dst, *ownedTable)) && !routeMatches(row, *ownedTable) {
				return fmt.Errorf("·�ɱ� %d �� %s �ѱ����������޸ģ�δ����", n.Table(), dst)
			}
		}
	}
	return nil
}

func Down(ctx context.Context, r Runner, s model.State) error {
	policy := false
	for _, n := range s.Nodes {
		policy = policy || n.PolicyRouting
	}
	if !policy {
		return nil
	}
	rows, err := readRules(ctx, r)
	if err != nil {
		return err
	}
	var first error
	for _, n := range s.Nodes {
		if !n.PolicyRouting {
			continue
		}
		foreign := false
		for _, row := range rows {
			priority := intValue(row["priority"])
			if (priority == n.SourcePriority() || priority == n.MarkPriority()) && !ruleMatches(row, n, priority == n.MarkPriority()) {
				foreign = true
			}
		}
		if foreign {
			if first == nil {
				first = fmt.Errorf("�ڵ� %d �Ĺ������ȼ������������ã�δɾ��", n.ID)
			}
			continue
		}
		routes, err := readTable(ctx, r, n.Table())
		if err != nil {
			if first == nil {
				first = err
			}
			continue
		}
		for _, cmd := range RouteCommands(n, false) {
			if cmd.Args[2] == "route" {
				safe := true
				for _, row := range routes {
					dst, _ := row["dst"].(string)
					wanted, parseErr := netip.ParsePrefix(cmd.Args[4])
					if parseErr == nil && wanted.Bits() == 32 && dst == wanted.Addr().String() {
						dst += "/32"
					}
					if dst == cmd.Args[4] && !routeMatches(row, n) {
						safe = false
					}
				}
				if !safe {
					if first == nil {
						first = fmt.Errorf("·�ɱ� %d �� %s �ѱ����������޸ģ�δɾ��", n.Table(), cmd.Args[4])
					}
					continue
				}
			}
			output, err := r.Run(ctx, cmd.Args...)
			if err != nil && !missingResource(string(output)) && first == nil {
				first = err
			}
		}
	}
	return first
}

func missingResource(output string) bool {
	for _, s := range []string{"No such process", "No such file", "Cannot find", "FIB table does not exist"} {
		if strings.Contains(output, s) {
			return true
		}
	}
	return false
}

// Up is idempotent for exact owned resources. No rule/table ranges are ever flushed.
func Up(ctx context.Context, r Runner, s model.State) error {
	if err := CheckConflicts(ctx, r, s, &s); err != nil {
		return err
	}
	if err := Down(ctx, r, s); err != nil {
		return err
	}
	for _, n := range s.Nodes {
		for _, cmd := range RouteCommands(n, true) {
			if _, err := r.Run(ctx, cmd.Args...); err != nil {
				cleanupErr := Down(ctx, r, s)
				if cleanupErr != nil {
					return fmt.Errorf("����·��ʧ�ܣ�%v������ʧ�ܣ�%w", err, cleanupErr)
				}
				return err
			}
		}
	}
	return nil
}

func Plan(s model.State) []Command {
	result := []Command{}
	for _, n := range s.Nodes {
		result = append(result, RouteCommands(n, true)...)
	}
	return result
}
