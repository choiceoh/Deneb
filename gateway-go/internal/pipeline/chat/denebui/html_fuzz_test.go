package denebui

import "testing"

// FuzzParseHTML hardens the tokenizer against adversarial/mangled markup: it
// must never panic or hang, and whatever tree it produces must be safe to run
// through the schema validator. Seeds cover the grammar's tricky corners; CI
// runs them as regular unit cases (go test), full fuzzing is on-demand:
//
//	go test -fuzz=FuzzParseHTML -fuzztime=30s ./internal/pipeline/chat/denebui/
func FuzzParseHTML(f *testing.F) {
	f.Add("<column><card><text>x</text></card></column>")
	f.Add(`<select id="p" label="선택"><option>가<option selected>나</select>`)
	f.Add("<markdown>&#96;&#96;&#96;go\na &lt; b\n&#96;&#96;&#96;</markdown>")
	f.Add("<table><tr><th>이름<th>값</tr><tr><td>구리<td>9,540</table>")
	f.Add(`<column><card><text style="body">잘림`)
	f.Add(`a < b <wat attr='x <card>`)
	f.Add(`<button event="e" data-k="v" collect="a,b">전송</button>`)
	f.Add("<!DOCTYPE html><!-- c --><chips id=\"t\"><chip value='a'>에")
	f.Add("<input type=checkbox id=c1 checked/><hr><img src=x>")
	f.Add("<tabs><tab label=\"1\"><tab label=\"2\"><text>겹침")
	f.Add("</closed-only></text>&#x1F600;&broken;&#0;&#99999999999;")
	f.Add("<code>if a < b { <card> } </code><textarea id=m>내용")
	f.Fuzz(func(t *testing.T, body string) {
		root, _ := ParseHTML(body) // must not panic
		if root != nil {
			_ = validateNode(root, "$") // must not panic either
		}
	})
}
