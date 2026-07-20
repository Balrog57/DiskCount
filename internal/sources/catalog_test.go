package sources

import (
	"testing"
)

func TestMindfactoryInfo(t *testing.T) {
	s := &Mindfactory{urls: []string{"https://x"}}
	info := s.Info()
	if info.Name != "mindfactory" {
		t.Fatalf("info.Name = %q", info.Name)
	}
	assertInfoValid(t, info)
}

func TestAlternateInfo(t *testing.T) {
	s := &Alternate{urls: []string{"https://x"}}
	info := s.Info()
	if info.Name != "alternate" {
		t.Fatalf("info.Name = %q", info.Name)
	}
	assertInfoValid(t, info)
}

func TestComputeruniverseInfo(t *testing.T) {
	s := &Computeruniverse{urls: []string{"https://x"}}
	info := s.Info()
	if info.Name != "computeruniverse" {
		t.Fatalf("info.Name = %q", info.Name)
	}
	assertInfoValid(t, info)
}

func TestProshopInfo(t *testing.T) {
	s := &Proshop{urls: []string{"https://x"}}
	info := s.Info()
	if info.Name != "proshop" {
		t.Fatalf("info.Name = %q", info.Name)
	}
	assertInfoValid(t, info)
}

func TestGeizhalsInfo(t *testing.T) {
	s := &Geizhals{urls: []string{"https://x"}}
	info := s.Info()
	if info.Name != "geizhals" {
		t.Fatalf("info.Name = %q", info.Name)
	}
	assertInfoValid(t, info)
}

func TestLDLCInfo(t *testing.T) {
	s := &LDLC{urls: []string{"https://x"}}
	info := s.Info()
	if info.Name != "ldlc" {
		t.Fatalf("info.Name = %q", info.Name)
	}
	assertInfoValid(t, info)
}

func TestTopachatInfo(t *testing.T) {
	s := &Topachat{urls: []string{"https://x"}}
	info := s.Info()
	if info.Name != "topachat" {
		t.Fatalf("info.Name = %q", info.Name)
	}
	assertInfoValid(t, info)
}

func TestGrosbillInfo(t *testing.T) {
	s := &Grosbill{urls: []string{"https://x"}}
	info := s.Info()
	if info.Name != "grosbill" {
		t.Fatalf("info.Name = %q", info.Name)
	}
	assertInfoValid(t, info)
}

func TestFnacInfo(t *testing.T) {
	s := &Fnac{urls: []string{"https://x"}}
	info := s.Info()
	if info.Name != "fnac" {
		t.Fatalf("info.Name = %q", info.Name)
	}
	assertInfoValid(t, info)
}

func TestBoulangerInfo(t *testing.T) {
	s := &Boulanger{urls: []string{"https://x"}}
	info := s.Info()
	if info.Name != "boulanger" {
		t.Fatalf("info.Name = %q", info.Name)
	}
	assertInfoValid(t, info)
}

func TestCdiscountInfo(t *testing.T) {
	s := &Cdiscount{urls: []string{"https://x"}}
	info := s.Info()
	if info.Name != "cdiscount" {
		t.Fatalf("info.Name = %q", info.Name)
	}
	assertInfoValid(t, info)
}

func TestRakutenInfo(t *testing.T) {
	s := &Rakuten{urls: []string{"https://x"}}
	info := s.Info()
	if info.Name != "rakuten" {
		t.Fatalf("info.Name = %q", info.Name)
	}
	assertInfoValid(t, info)
}

func TestRueDuCommerceInfo(t *testing.T) {
	s := &RueDuCommerce{urls: []string{"https://x"}}
	info := s.Info()
	if info.Name != "rueducommerce" {
		t.Fatalf("info.Name = %q", info.Name)
	}
	assertInfoValid(t, info)
}

func TestBackmarketInfo(t *testing.T) {
	s := &Backmarket{urls: []string{"https://x"}}
	info := s.Info()
	if info.Name != "backmarket" {
		t.Fatalf("info.Name = %q", info.Name)
	}
	assertInfoValid(t, info)
}

func assertInfoValid(t *testing.T, info SourceInfo) {
	t.Helper()
	if info.Description == "" {
		t.Error("Info.Description must not be empty")
	}
	if len(info.Categories) == 0 {
		t.Error("Info.Categories must have at least one entry")
	}
	if len(info.Requires) == 0 {
		t.Error("Info.Requires must list at least one env key")
	}
	if info.Version == "" {
		t.Error("Info.Version must not be empty")
	}
}
