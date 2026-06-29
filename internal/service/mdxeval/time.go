package mdxeval

import (
	"context"
	"fmt"

	"cubodw/internal/engine/mdx"
	"cubodw/internal/engine/metadata"
	"cubodw/internal/engine/query"
	"cubodw/internal/service/queryexec"
)

// Funções de inteligência de tempo (navegação sobre os membros ordenados de um
// nível): PrevMember, NextMember, Lag(n), Lead(n), YTD. Trabalham dentro do
// contexto dos ancestrais do membro (ex.: trimestres dentro do mesmo ano).

// memberContext resolve o membro e devolve a lista ordenada de irmãos (membros
// do mesmo nível sob os mesmos ancestrais) e a posição do membro nela.
func memberContext(ctx context.Context, cube *metadata.Cube, exec *queryexec.Service, id *mdx.Id) (query.LevelRef, []query.Filter, []string, int, error) {
	ref, err := resolveMemberId(cube, id)
	if err != nil {
		return query.LevelRef{}, nil, nil, 0, err
	}
	lvl := ref.hier.Levels[ref.levelIndex]
	levelRef := query.LevelRef{Dimension: ref.dim.Name, Level: lvl.Name}

	var ancestors []query.Filter
	for li, v := range ref.values {
		if li == ref.levelIndex {
			continue
		}
		ancestors = append(ancestors, query.Filter{
			Dimension: ref.dim.Name, Level: ref.hier.Levels[li].Name, Members: []string{v},
		})
	}

	members, err := exec.EnumerateLevel(ctx, cube, levelRef, ancestors)
	if err != nil {
		return query.LevelRef{}, nil, nil, 0, err
	}
	own := ref.values[ref.levelIndex]
	idx := -1
	for i, m := range members {
		if m == own {
			idx = i
			break
		}
	}
	if idx < 0 {
		return query.LevelRef{}, nil, nil, 0, fmt.Errorf("membro %q não encontrado no nível %q", own, lvl.Name)
	}
	return levelRef, ancestors, members, idx, nil
}

// timeShift devolve o membro deslocado por `shift` posições (PrevMember=-1,
// NextMember=+1, Lag(n)=-n, Lead(n)=+n). Fora do intervalo → conjunto vazio.
func timeShift(ctx context.Context, cube *metadata.Cube, exec *queryexec.Service, id *mdx.Id, shift int) ([]levelBinding, []position, error) {
	levelRef, ancestors, members, idx, err := memberContext(ctx, cube, exec, id)
	if err != nil {
		return nil, nil, err
	}
	binding := levelBinding{ref: levelRef, filters: ancestors}
	target := idx + shift
	if target < 0 || target >= len(members) {
		return []levelBinding{binding}, []position{}, nil
	}
	return []levelBinding{binding}, []position{{values: []string{members[target]}}}, nil
}

// ytd devolve os membros do começo do ciclo (sob os ancestrais) até o membro,
// inclusive — Year-To-Date quando o ancestral é o ano.
func ytd(ctx context.Context, cube *metadata.Cube, exec *queryexec.Service, id *mdx.Id) ([]levelBinding, []position, error) {
	levelRef, ancestors, members, idx, err := memberContext(ctx, cube, exec, id)
	if err != nil {
		return nil, nil, err
	}
	binding := levelBinding{ref: levelRef, filters: ancestors}
	pos := make([]position, 0, idx+1)
	for i := 0; i <= idx; i++ {
		pos = append(pos, position{values: []string{members[i]}})
	}
	return []levelBinding{binding}, pos, nil
}
