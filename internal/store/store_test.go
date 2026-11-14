package store

import (
	"testing"
)

func TestOpenMigrateAndDefaultTenant(t *testing.T) {
	dir := t.TempDir()
	db, err := Open(dir)
	if err != nil {
		t.Fatalf("打开数据库失败: %v", err)
	}
	id, err := DefaultTenantID(db)
	if err != nil || id == 0 {
		t.Fatalf("默认租户应存在: id=%d err=%v", id, err)
	}
	// Windows 下必须显式关闭句柄，TempDir 才能清理
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlDB.Close(); err != nil {
		t.Fatal(err)
	}

	// 重复打开不重复创建
	db2, err := Open(dir)
	if err != nil {
		t.Fatalf("二次打开失败: %v", err)
	}
	id2, err := DefaultTenantID(db2)
	if err != nil || id2 != id {
		t.Fatalf("默认租户应保持唯一: %d != %d", id, id2)
	}
	if sqlDB2, err := db2.DB(); err == nil {
		sqlDB2.Close()
	}
}
