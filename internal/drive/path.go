package drive

import (
	"context"
	"errors"
	"fmt"
	"path"
	"strings"
)

// ErrNotFound は Resolve でリテラルパスが存在しなかったことを表す。
// errors.Is で判定し、API 障害などの本当のエラーと区別するために使う。
var ErrNotFound = errors.New("パスが見つかりません")

// Node はパス解決の結果一つ分。Drive 上のファイルと、その絶対パスを持つ。
// 同名ファイルや glob により一つのパス式が複数の Node に解決されうる。
type Node struct {
	File File
	// Path はマイドライブ起点の絶対パス ("/" 始まり)。
	// glob 展開後は実体のファイル名で構成された具体パスになる。
	Path string
	// ParentID は親フォルダの ID。mv (親の付け替え) で旧親を指定するために使う。
	// ルート自身の Node では空。
	ParentID string
}

// splitPath はマイドライブ起点のパスを要素列に分解する。
// 先頭/末尾のスラッシュや空要素 ("//" など) は無視する。
// ルート ("/" や "") は空スライスを返す。
func splitPath(p string) []string {
	parts := strings.Split(p, "/")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part == "" {
			continue
		}
		out = append(out, part)
	}
	return out
}

// hasMeta はパス要素が glob メタ文字を含むかを返す。
func hasMeta(component string) bool {
	return strings.ContainsAny(component, "*?[")
}

// Resolve はマイドライブ起点のパス式を解決し、マッチした Node 群を返す。
//
// パスにワイルドカードを含む場合は階層ごとに候補を展開する。マッチが
// 一件も無ければ空スライスを返す (エラーにはしない)。リテラルパスで
// 存在しない場合のみ NotFound 相当のエラーを返す。
func (c *Client) Resolve(ctx context.Context, p string) ([]Node, error) {
	rootID, err := c.RootID(ctx)
	if err != nil {
		return nil, err
	}
	components := splitPath(p)

	// ルート自身を指す場合。
	root := Node{
		File: File{ID: rootID, Name: "", MimeType: folderMIME},
		Path: "/",
	}
	if len(components) == 0 {
		return []Node{root}, nil
	}

	current := []Node{root}
	for i, comp := range components {
		isLast := i == len(components)-1
		next, err := c.expandComponent(ctx, current, comp, isLast)
		if err != nil {
			return nil, err
		}
		if len(next) == 0 {
			// glob を含む中間/末尾でマッチ無しは「結果ゼロ」。
			// リテラルのみで構成され、かつ展開途中で消えた場合は not found。
			if hasMetaAnywhere(components) {
				return nil, nil
			}
			// ErrNotFound でラップし、呼び出し側が「単に存在しない」ことを
			// API 障害などの本当のエラーと区別できるようにする。
			return nil, fmt.Errorf("%w: %s", ErrNotFound, p)
		}
		current = next
	}
	return current, nil
}

// expandComponent は現在の Node 群それぞれの直下から、comp にマッチする
// 子要素を集めて次段の Node 群を作る。
//
// comp がメタ文字を含む場合は ListChildren して path.Match でフィルタし、
// 含まない場合は ListChildrenByName で完全一致を引く (API 負荷が軽い)。
func (c *Client) expandComponent(ctx context.Context, current []Node, comp string, isLast bool) ([]Node, error) {
	var next []Node
	literal := !hasMeta(comp)

	for _, parent := range current {
		// 親がフォルダでなければ、その配下は辿れない (リーフは展開対象外)。
		if !parent.File.IsFolder() {
			continue
		}

		var children []File
		var err error
		if literal {
			children, err = c.ListChildrenByName(ctx, parent.File.ID, comp)
		} else {
			children, err = c.ListChildren(ctx, parent.File.ID)
		}
		if err != nil {
			return nil, err
		}

		for _, child := range children {
			if !literal {
				matched, merr := path.Match(comp, child.Name)
				if merr != nil {
					return nil, fmt.Errorf("不正なワイルドカードパターン %q: %w", comp, merr)
				}
				if !matched {
					continue
				}
			}
			next = append(next, Node{
				File:     child,
				Path:     joinPath(parent.Path, child.Name),
				ParentID: parent.File.ID,
			})
		}
	}
	return next, nil
}

// joinPath はマイドライブ起点パスに子要素名を連結する。
func joinPath(parent, name string) string {
	if parent == "/" {
		return "/" + name
	}
	return parent + "/" + name
}

// SplitParent はマイドライブ起点の絶対パスを (親フォルダの絶対パス, 末尾要素名)
// に分ける。アップロード先やリネーム先の決定に使う。
//
//	"/a/b/c" -> ("/a/b", "c")
//	"/a"     -> ("/",    "a")
//	"/"      -> ("",     "")   (ルート自身に親は無い)
func SplitParent(absPath string) (parent, name string) {
	comps := splitPath(absPath)
	if len(comps) == 0 {
		return "", ""
	}
	name = comps[len(comps)-1]
	parentComps := comps[:len(comps)-1]
	if len(parentComps) == 0 {
		return "/", name
	}
	return "/" + strings.Join(parentComps, "/"), name
}

// hasMetaAnywhere はパス要素のいずれかが glob メタ文字を含むかを返す。
func hasMetaAnywhere(components []string) bool {
	for _, c := range components {
		if hasMeta(c) {
			return true
		}
	}
	return false
}
