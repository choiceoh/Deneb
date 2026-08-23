package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/choiceoh/deneb/gateway-go/internal/domain/wiki"
)

const kia002Rep = "프로젝트/pl2-kia-epc-002/대표.md"
const kia002Bid = "프로젝트/pl2-kia-epc-002/기아-al광주-2공장-태양광-모듈-입찰.md"
const kia003Rep = "프로젝트/pl2-kia-epc-003/대표.md"

var repMergeRemnantPaths = []string{
	"프로젝트/pl2-kia-epc-001/대표.md",
	"프로젝트/pl2-dsv-epc-001/대표.md",
	"프로젝트/nde-sun-cbl-001/대표.md",
	"프로젝트/pl1-wdo-dev-001/대표.md",
	"프로젝트/pl1-mrp-dev-001/대표.md",
	"프로젝트/금호타이어/대표.md",
}

const mergeRemnantHeading = "## 병합된 중복 문서"

func foldMergeRemnantBody(body string) (string, bool) {
	i := strings.Index(body, mergeRemnantHeading)
	if i < 0 {
		return body, false
	}
	return strings.TrimRight(body[:i], "\n") + "\n", true
}

func ensureKia002Rep(store *wiki.Store, now time.Time, apply bool, rep *report) error {
	if exists(store, kia002Rep) {
		rep.add("skip %s (already exists)", kia002Rep)
		return retargetKia003Related(store, apply, rep)
	}
	if !apply {
		rep.add("would write %s", kia002Rep)
		return retargetKia003Related(store, apply, rep)
	}
	page := wiki.NewPage("기아 AL광주 2공장 태양광", "프로젝트",
		[]string{"기아", "광주", "캐노피", "모듈"})
	page.Meta.ID = "pl2-kia-epc-002"
	page.Meta.Code = "pl2-kia-epc-002"
	page.Meta.Summary = "광주2공장 물류통로 캐노피 309.7kW — 8/20 공급가액 858,000,000 제출, 8/24 통합견적 재요청. 모듈 RFP(5/11 Jinko 640W)는 탈락(JA 낙찰)."
	page.Meta.Related = []string{
		"프로젝트/pl2-kia-epc-001/대표.md",
		kia003Rep,
		kia002Bid,
	}
	page.Meta.Cues = []string{"AL광주", "광주2공장", "물류통로", "이현성", "캐노피"}
	page.Meta.Client = "기아"
	page.Meta.Sites = []string{"광주 광산구"}
	page.Meta.Kinds = []string{"태양광/루프탑"}
	page.Meta.Type = "project"
	page.Meta.Confidence = "high"
	page.Meta.Importance = 0.85
	page.Meta.Due = "2026-08-24"
	day := now.Format("2006-01-02")
	page.Meta.Updated = day
	page.Body = kia002RepBody(day)
	if err := store.WritePage(kia002Rep, page); err != nil {
		return fmt.Errorf("write %s: %w", kia002Rep, err)
	}
	rep.add("wrote %s", kia002Rep)
	if err := retargetKiaBidRelated(store, apply, rep); err != nil {
		return err
	}
	return retargetKia003Related(store, apply, rep)
}

func kia002RepBody(day string) string {
	return `# 기아 AL광주 2공장 태양광

화성([[프로젝트/pl2-kia-epc-001/대표.md]])·광명([[프로젝트/pl2-kia-epc-003/대표.md]])과 다른 건. 광주 2공장.

## 지금

- **물류통로 캐노피 309.7kW** — 8/20 견적 제출, 공급가액 858,000,000. 기아 이현성 책임: 수배전반·전기공사 고가 지적, 8/24 통합견적 재제출.
- **모듈 RFP** (사원주차장·차량 POOL장) — 5/11 Jinko 640W 제출, JA 낙찰, 당사 탈락. 상세 [[` + kia002Bid + `]].
- **5MW EPC** — 선그로우 센트럴 인버터 예가. 탑솔라 공급가 3,300만원/MW (외부 3,900만원/MW). 6/2 수령, 6/15 재공유.

## 담당

- 기아 광주시설관리팀: 이현성 2555151@kia.com, 류창준, 김재민. 인프라구매 황세호.
- 탑솔라: 김대희, 김성훈.

## 슬롯

- 입찰 상세: [[` + kia002Bid + `]]
- 로그: [[프로젝트/pl2-kia-epc-002/로그.md]]
- 현장: [[프로젝트/pl2-kia-epc-002/현장/광주.md]]

## 변경 이력

- ` + day + `: P2 대표 신설. 001 대표에 붙어 있던 광주 5MW 병합 잔재를 이 장으로 모음.
`
}

func retargetKia003Related(store *wiki.Store, apply bool, rep *report) error {
	if !exists(store, kia003Rep) {
		return nil
	}
	if !apply {
		rep.add("would retarget %s related → %s", kia003Rep, kia002Rep)
		return nil
	}
	err := store.UpdatePage(kia003Rep, func(page *wiki.Page) (*wiki.Page, error) {
		if page == nil {
			return nil, nil
		}
		changed := false
		for i, r := range page.Meta.Related {
			if r == kia002Bid {
				page.Meta.Related[i] = kia002Rep
				changed = true
			}
		}
		if !changed {
			return nil, nil
		}
		return page, nil
	})
	if err != nil {
		return fmt.Errorf("retarget %s: %w", kia003Rep, err)
	}
	rep.add("retargeted %s related → %s", kia003Rep, kia002Rep)
	return nil
}

func retargetKiaBidRelated(store *wiki.Store, apply bool, rep *report) error {
	if !exists(store, kia002Bid) || !apply {
		return nil
	}
	err := store.UpdatePage(kia002Bid, func(page *wiki.Page) (*wiki.Page, error) {
		if page == nil {
			return nil, nil
		}
		page.Meta.Related = wiki.RankRelated([]string{
			kia002Rep,
			"프로젝트/pl2-kia-epc-001/대표.md",
			kia003Rep,
		}, wiki.MaxRelatedPerPage)
		return page, nil
	})
	if err != nil {
		return fmt.Errorf("retarget %s: %w", kia002Bid, err)
	}
	rep.add("retargeted %s related → 대표", kia002Bid)
	return nil
}

func foldRepMergeRemnants(store *wiki.Store, apply bool, rep *report) error {
	for _, path := range repMergeRemnantPaths {
		page, err := store.ReadPage(path)
		if err != nil || page == nil {
			rep.add("skip missing %s", path)
			continue
		}
		_, changed := foldMergeRemnantBody(page.Body)
		if !changed {
			rep.add("skip %s (no merge remnant)", path)
			continue
		}
		if !apply {
			rep.add("would fold remnant on %s", path)
			continue
		}
		err = store.UpdatePage(path, func(p *wiki.Page) (*wiki.Page, error) {
			if p == nil {
				return nil, nil
			}
			body, ok := foldMergeRemnantBody(p.Body)
			if !ok {
				return nil, nil
			}
			p.Body = body
			return p, nil
		})
		if err != nil {
			return fmt.Errorf("fold remnant %s: %w", path, err)
		}
		rep.add("folded remnant on %s", path)
	}
	return nil
}
