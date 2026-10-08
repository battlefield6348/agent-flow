package orchestrator

import (
	"reflect"
	"testing"
)

func TestCollaboratorConfigEffectiveRole(t *testing.T) {
	cases := []struct {
		name string
		col  CollaboratorConfig
		want string
	}{
		{"明確指定 role", CollaboratorConfig{ID: "reviewer-b", Role: "reviewer"}, RoleReviewer},
		{"role 大小寫與空白", CollaboratorConfig{ID: "x", Role: " Coder "}, RoleCoder},
		{"未指定 role 時沿用 id 以相容舊設定", CollaboratorConfig{ID: "reviewer"}, RoleReviewer},
		{"未知角色", CollaboratorConfig{ID: "helper"}, "helper"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.col.EffectiveRole(); got != tc.want {
				t.Errorf("EffectiveRole() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestCollaboratorConfigSessionNames(t *testing.T) {
	cases := []struct {
		name string
		col  CollaboratorConfig
		want []string
	}{
		{"單一 session 沿用原名稱", CollaboratorConfig{ID: "reviewer", CaoSessionName: "gitlab-reviewer"}, []string{"cao-gitlab-reviewer"}},
		{"未指定 session 名稱時依 id 產生", CollaboratorConfig{ID: "reviewer"}, []string{"cao-gitlab-reviewer"}},
		{"replicas 產生編號 session", CollaboratorConfig{ID: "reviewer", CaoSessionName: "gitlab-reviewer", Replicas: 3},
			[]string{"cao-gitlab-reviewer-1", "cao-gitlab-reviewer-2", "cao-gitlab-reviewer-3"}},
		{"coder 共用本機 Repo，replicas 強制為 1", CollaboratorConfig{ID: "coder", Replicas: 3}, []string{"cao-gitlab-coder"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.col.SessionNames(); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("SessionNames() = %v, want %v", got, tc.want)
			}
		})
	}
}
