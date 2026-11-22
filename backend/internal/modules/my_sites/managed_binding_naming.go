package my_sites

import (
	"context"
	"fmt"
	"math"
	"net"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/net/idna"
	"golang.org/x/net/publicsuffix"
	"golang.org/x/text/unicode/norm"

	"transithub/backend/internal/modules/upstream"
)

func bindingSlug(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	if net.ParseIP(host) != nil {
		return ""
	}
	host, err = idna.Lookup.ToASCII(host)
	if err != nil {
		return ""
	}
	suffix, public := publicsuffix.PublicSuffix(host)
	if !public && !strings.Contains(suffix, ".") {
		return ""
	}
	domain, err := publicsuffix.EffectiveTLDPlusOne(host)
	if err != nil {
		return ""
	}
	slug, _, _ := strings.Cut(domain, ".")
	return slug
}

func managedGroupName(value string) string {
	var name strings.Builder
	separator := false
	for _, r := range norm.NFKC.String(value) {
		if unicode.IsLetter(r) || unicode.IsNumber(r) || unicode.IsMark(r) {
			if separator && name.Len() > 0 {
				name.WriteByte('-')
			}
			name.WriteRune(r)
			separator = false
		} else {
			separator = true
		}
	}
	return name.String()
}

func managedBindingName(slug, group string, rate *float64, number int, platform upstream.Platform) (string, error) {
	if slug == "" {
		return "", fmt.Errorf("缺少上游域名简称")
	}
	group = managedGroupName(group)
	if group == "" {
		return "", fmt.Errorf("缺少自有分组名")
	}
	if rate == nil || *rate < 0 || math.IsNaN(*rate) || math.IsInf(*rate, 0) {
		return "", fmt.Errorf("缺少上游分组倍率")
	}
	suffix := "-" + strconv.FormatFloat(*rate, 'f', -1, 64) + fmt.Sprintf("x-%03d", number)
	if platform == upstream.PlatformNewAPI {
		allowed := 50 - len(slug) - 1 - len(suffix)
		if allowed < 1 {
			return "", fmt.Errorf("简称或倍率超出 New API 名称长度限制")
		}
		if len(group) > allowed {
			for allowed > 0 && !utf8.RuneStart(group[allowed]) {
				allowed--
			}
			group = strings.TrimRight(group[:allowed], "-")
			if group == "" {
				return "", fmt.Errorf("分组名超出 New API 名称长度限制")
			}
		}
	}
	return slug + "-" + group + suffix, nil
}

func managedRate(site *upstream.Site, group string) *float64 {
	if site == nil {
		return nil
	}
	for _, entry := range site.Metrics.Groups {
		if entry.ID == group {
			return entry.Multiplier
		}
	}
	return nil
}

type bindingNameRow struct {
	ConnectionID string
	GroupID      string
	Slug         string
	Number       int
	Name         string
}

func (m *KeyManagement) reserveBindingNames(ctx context.Context, uid, aid string, requested []bindingNameRow) ([]bindingNameRow, error) {
	tx, err := m.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", "binding-names|"+uid+"|"+aid); err != nil {
		return nil, err
	}
	for i := range requested {
		row := &requested[i]
		if row.Slug == "" {
			continue
		}
		_, err = tx.Exec(ctx, `INSERT INTO managed_binding_names(user_id,workspace_id,connection_id,own_group_id,slug,ordinal)
			SELECT $1,$2,$3,$4,$5,coalesce(max(ordinal),0)+1 FROM managed_binding_names WHERE user_id=$1 AND workspace_id=$2 AND slug=$5
			ON CONFLICT(user_id,workspace_id,connection_id,own_group_id) DO NOTHING`, uid, aid, row.ConnectionID, row.GroupID, row.Slug)
		if err != nil {
			return nil, err
		}
		err = tx.QueryRow(ctx, `SELECT slug,ordinal,applied_name FROM managed_binding_names WHERE user_id=$1 AND workspace_id=$2 AND connection_id=$3 AND own_group_id=$4`, uid, aid, row.ConnectionID, row.GroupID).Scan(&row.Slug, &row.Number, &row.Name)
		if err != nil {
			return nil, err
		}
	}
	return requested, tx.Commit(ctx)
}

func orderedBindings(entries []RealConnection) {
	slices.SortFunc(entries, func(a, b RealConnection) int {
		if byTime := strings.Compare(a.CreatedAt, b.CreatedAt); byTime != 0 {
			return byTime
		}
		return strings.Compare(a.ID, b.ID)
	})
}

func (m *KeyManagement) skipOccupiedOrdinal(ctx context.Context, uid, aid string, row bindingNameRow) (int, error) {
	tx, err := m.db.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, "SELECT pg_advisory_xact_lock(hashtextextended($1,0))", "binding-names|"+uid+"|"+aid); err != nil {
		return 0, err
	}
	var next int
	err = tx.QueryRow(ctx, "UPDATE managed_binding_names SET ordinal=(SELECT coalesce(max(n.ordinal),0)+1 FROM managed_binding_names n WHERE n.user_id=$1 AND n.workspace_id=$2 AND n.slug=$5) WHERE user_id=$1 AND workspace_id=$2 AND connection_id=$3 AND own_group_id=$4 AND slug=$5 AND applied_name='' AND NOT EXISTS(SELECT 1 FROM managed_binding_progress p WHERE p.user_id=$1 AND p.workspace_id=$2 AND p.operation_id=$3 AND p.step NOT IN('','done')) RETURNING ordinal", uid, aid, row.ConnectionID, row.GroupID, row.Slug).Scan(&next)
	if err != nil {
		return 0, fmt.Errorf("目标名称已被占用，不能更改已发布或正在使用的编号")
	}
	return next, tx.Commit(ctx)
}
