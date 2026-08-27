package migrate

/*
注册表快照 —— 供「某条迁移是否真的接进了启动链」这类接线断言使用。

为什么要快照：BeforeAutoMigrate / AfterAutoMigrate 跑完会把对应的注册表置 nil
（每进程只跑一次）。任何测试一旦调过它们，后面的测试直接读注册表就只能看到 nil，
断言会随测试顺序变成假绿或假红。init() 一定早于本包任何测试函数，所以这里拿到的是
「全部 init() 注册完、还没有人跑过迁移」的状态。
*/

import "sort"

type registrySnapshot struct {
	before []int
	after  []int
}

var registeredVersions registrySnapshot

func init() {
	for _, m := range beforeAutoMigrations {
		registeredVersions.before = append(registeredVersions.before, m.Version)
	}
	for _, m := range afterAutoMigrations {
		registeredVersions.after = append(registeredVersions.after, m.Version)
	}
	sort.Ints(registeredVersions.before)
	sort.Ints(registeredVersions.after)
}

func (s registrySnapshot) hasBefore(v int) bool { return containsInt(s.before, v) }
func (s registrySnapshot) hasAfter(v int) bool  { return containsInt(s.after, v) }

func containsInt(xs []int, want int) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}
