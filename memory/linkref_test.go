package memory

import "testing"

func TestParseWikiLinksKeepsHeadingBlockDisplayAndEmbed(t *testing.T) {
	text := "见 [[API]]、[[登录#失败原因]]、[[登录#^block-9]]、[[登录|登录流程]]。\n嵌入 ![[登录]]。\n同时 [[登录#失败原因#^block-9|显示]]。"
	links := parseWikiLinks(text)
	if len(links) != 6 {
		t.Fatalf("links = %d, want 6: %+v", len(links), links)
	}
	want := []WikiLink{
		{Target: "API"},
		{Target: "登录", Heading: "失败原因"},
		{Target: "登录", Block: "block-9"},
		{Target: "登录", Display: "登录流程"},
		{Target: "登录", Embed: true},
		{Target: "登录", Heading: "失败原因", Block: "block-9", Display: "显示"},
	}
	for i, link := range want {
		got := links[i]
		if got.Target != link.Target || got.Heading != link.Heading || got.Block != link.Block || got.Display != link.Display || got.Embed != link.Embed {
			t.Fatalf("link %d = %+v, want %+v", i, got, link)
		}
	}
	names := extractWikiLinks(text)
	if len(names) != 2 || names[0] != "API" || names[1] != "登录" {
		t.Fatalf("note names = %#v, want API and 登录 only", names)
	}
}

func TestParseWikiLinksSkipsEmptyTargets(t *testing.T) {
	links := parseWikiLinks("[[#只有标题]] [[]] [[ |显示]]")
	if len(links) != 0 {
		t.Fatalf("empty targets produced links: %+v", links)
	}
}
