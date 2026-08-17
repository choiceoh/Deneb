/*
 * 탭별 화면 렌더러 — 상태를 받아 HTML 문자열을 돌려주는 순수 함수 모음.
 * DOM 이벤트 배선은 ui.js가 위임 방식으로 처리한다(재렌더에도 핸들러가 살아있도록).
 */
(function (root) {
  'use strict';

  const { SEGMENTS, SEGMENT_ORDER, MATERIALS, CONFIG } = root.AirlinerData;
  const E = root.AirlinerEngine;
  const D = root.AirlinerDesign;
  const B = root.AirlinerBidding;

  const money = E.fmtMoney;
  const num = (n) => Math.round(n).toLocaleString('ko-KR');
  const esc = (t) =>
    String(t).replace(/[&<>"']/g, (c) => ({ '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;', "'": '&#39;' }[c]));

  function bar(pct, cls) {
    const v = Math.max(0, Math.min(100, pct));
    return `<div class="bar"><span class="${cls || ''}" style="width:${v}%"></span></div>`;
  }

  function phaseLabel(p) {
    return { dev: '개발 중', cert: '형식증명 심사', production: '양산', cancelled: '중단' }[p.phase] || p.phase;
  }

  /** 기종별 미인도 주문 잔량 */
  function orderedBy(s, programId) {
    return s.backlog.reduce((a, o) => a + (o.programId === programId ? o.remaining : 0), 0);
  }

  // ─────────────────────────────── 개요 ───────────────────────────────

  function renderOverview(s) {
    const warnings = [];

    for (const p of s.programs.filter((x) => x.phase === 'production')) {
      const ordered = orderedBy(s, p.id);
      const lines = s.lines.filter((l) => l.programId === p.id).length;
      if (ordered > 0 && lines === 0) {
        warnings.push(
          `<b>${esc(p.name)}</b> 수주 잔고 ${ordered}기가 있는데 <b>조립 라인이 없다</b>. 라인을 세우기 전에는 한 대도 인도할 수 없다.`,
        );
      }
    }
    for (const p of s.programs.filter((x) => x.phase === 'dev' && x.share <= 0)) {
      warnings.push(`<b>${esc(p.name)}</b> 개발이 <b>동결</b> 상태다. 인력을 배분해야 진행된다.`);
    }
    if (s.cash < 400) warnings.push(`현금이 ${money(s.cash)}까지 떨어졌다. 차입·재고 처분·개발 동결을 검토하라.`);
    if (s.debt >= CONFIG.maxDebt * 0.9) warnings.push(`차입이 한도(${money(CONFIG.maxDebt)})에 근접했다. 한 번 더 적자가 나면 파산이다.`);
    const idleLines = s.lines.filter((l) => l.idle).length;
    if (idleLines) warnings.push(`가동 중지된 라인이 ${idleLines}개 있다. 유지비는 계속 나간다.`);

    const events = s.events.length
      ? s.events.map((e) => `<li><b>${esc(e.name)}</b> — ${esc(e.text)}</li>`).join('')
      : '<li class="muted">특별한 일 없이 지나간 분기.</li>';

    const last = s.history[s.history.length - 1];

    return `
      <section class="grid2">
        <div class="card">
          <h3>이번 분기 소식</h3>
          <ul class="events">${events}</ul>
        </div>
        <div class="card">
          <h3>경영 경고</h3>
          ${
            warnings.length
              ? `<ul class="warns">${warnings.map((w) => `<li>${w}</li>`).join('')}</ul>`
              : '<p class="muted">지금 당장 손봐야 할 문제는 없다.</p>'
          }
        </div>
      </section>

      <section class="cards">
        ${statCard('수주 잔고', num(E.totalBacklog(s)) + '기', money(E.backlogValue(s)) + ' 상당')}
        ${statCard('누적 인도', num(s.stats.delivered) + '기', '시장 점유율 ' + (E.marketShare(s) * 100).toFixed(1) + '%')}
        ${statCard('순자산', money(E.netWorth(s)), '현금 ' + money(s.cash) + ' · 부채 ' + money(s.debt))}
        ${statCard('평판', Math.round(s.reputation) + ' / 100', '엔지니어 ' + num(s.engineers) + '명')}
        ${
          last
            ? statCard(
                '전분기 손익',
                (last.net >= 0 ? '+' : '') + money(last.net),
                '매출 ' + money(last.revenue) + ' · 비용 ' + money(last.cost),
                last.net >= 0 ? 'good' : 'bad',
              )
            : statCard('전분기 손익', '—', '아직 정산 전')
        }
        ${statCard('보유 기종', s.programs.filter((p) => p.phase === 'production').length + '종', s.lines.length + '개 라인 가동')}
      </section>

      <section class="card">
        <h3>경쟁 구도</h3>
        <p class="muted">지금 각 시장에서 우리가 이겨야 하는 상대다. 경쟁사는 실제 역사대로 신형을 내놓는다.</p>
        <table class="spec">
          ${SEGMENT_ORDER.map((seg) => {
            const sg = SEGMENTS[seg];
            const o = B.bestOffering(s, seg, Math.round(sg.seats.ref), Math.round(sg.range.ref));
            const mine = s.programs.filter((p) => p.segment === seg && p.phase === 'production');
            const ours = mine.length ? mine.map((p) => esc(p.name)).join(', ') : '<span class="muted">없음</span>';
            return `<tr><th>${sg.name}</th><td>${o ? esc(o.name) : '—'} <span class="muted">· 우리: ${ours}</span></td></tr>`;
          }).join('')}
        </table>
      </section>

      <section class="card">
        <h3>최근 기록</h3>
        <ul class="log">${s.log.slice(0, 6).map(logItem).join('')}</ul>
      </section>`;
  }

  function statCard(label, value, sub, tone) {
    return `<div class="stat ${tone || ''}"><span class="stat-label">${label}</span><span class="stat-value">${value}</span><span class="stat-sub">${sub || ''}</span></div>`;
  }

  function logItem(l) {
    return `<li class="log-${l.kind}"><span class="log-turn">${l.label}</span> ${esc(l.text)}</li>`;
  }

  // ─────────────────────────────── 설계 ───────────────────────────────

  function renderDesign(s, spec, designName) {
    const seg = SEGMENTS[spec.segment];
    const segTabs = SEGMENT_ORDER.map(
      (id) =>
        `<button class="seg ${id === spec.segment ? 'on' : ''}" data-action="design-seg" data-seg="${id}">
           <b>${SEGMENTS[id].name}</b><span>${SEGMENTS[id].seats.min}–${SEGMENTS[id].seats.max}석</span>
         </button>`,
    ).join('');

    const mats = Object.values(MATERIALS)
      .map(
        (m) =>
          `<button class="mat ${m.id === spec.material ? 'on' : ''}" data-action="design-mat" data-mat="${m.id}">
             <b>${m.name}</b><span>${m.desc}</span>
           </button>`,
      )
      .join('');

    const derivatives = s.programs
      .filter((p) => p.phase === 'production')
      .map(
        (p) =>
          `<button class="ghost" data-action="derive" data-id="${p.id}">${esc(p.name)} 파생형 설계</button>`,
      )
      .join('');

    return `
      <section class="card">
        <h3>세그먼트</h3>
        <p class="muted">${esc(seg.desc)}</p>
        <div class="segs">${segTabs}</div>
      </section>

      <section class="grid2">
        <div class="card">
          <h3>제원</h3>
          ${slider('seats', '좌석수', spec.seats, seg.seats.min, seg.seats.max, 1, '석')}
          ${slider('range', '항속거리', spec.range, seg.range.min, seg.range.max, 100, 'km')}
          ${slider('tech', '기술 투자', spec.tech, 0, 100, 1, '')}
          <p class="hint">기술 투자를 올리면 연비·정가가 오르지만 개발비·기간·결함 위험이 함께 오른다.</p>
          <h3 style="margin-top:18px">주 구조재</h3>
          <div class="mats">${mats}</div>
        </div>
        <div class="card" id="design-preview">${renderDesignPreview(s, spec, designName)}</div>
      </section>

      ${derivatives ? `<section class="card"><h3>파생형</h3><p class="muted">기존 형식증명을 물려받아 개발비 66%, 기간 50%를 아낀다.</p><div class="row">${derivatives}</div></section>` : ''}`;
  }

  function slider(key, label, value, min, max, step, unit) {
    return `<label class="slider">
      <span>${label}<b id="lbl-${key}">${num(value)}${unit}</b></span>
      <input type="range" data-action="design-input" data-key="${key}" min="${min}" max="${max}" step="${step}" value="${value}">
    </label>`;
  }

  /** 설계 미리보기 — 슬라이더를 움직일 때 이 영역만 갈아끼운다. */
  function renderDesignPreview(s, spec, designName) {
    const ev = D.evaluate(spec);
    const upfront = Math.round(ev.devCost * CONFIG.launchUpfrontRate);
    const seg = SEGMENTS[spec.segment];

    // 현재 인력으로 실제 얼마나 걸리는지 — 표시 기간은 인력 100% 가정이라 오해를 부른다.
    // 엔진은 배분 가중치의 합으로 인력을 나누므로, 균등 배분이 아니라 실제 가중치로 계산해야
    // 미리보기와 실제 진행 속도가 어긋나지 않는다 (기존이 5/95 같은 편중이면 차이가 크다).
    const existingShare = s.programs
      .filter((p) => p.phase === 'dev' && p.share > 0)
      .reduce((a, p) => a + p.share, 0);
    const newShare = CONFIG.defaultProgramShare;
    const shareIfLaunched = newShare / (existingShare + newShare);
    const effective = Math.min(1.4, (s.engineers * shareIfLaunched) / ev.engineersNeeded);
    const realQuarters = effective > 0 ? Math.ceil(ev.devQuarters / effective) : Infinity;

    const affordable = s.cash >= upfront;
    const liquidity = s.cash + Math.max(0, CONFIG.maxDebt - s.debt);
    const heavy = ev.devCost > liquidity;

    return `
      <h3>설계 평가</h3>
      <table class="spec">
        <tr><th>총 개발비</th><td class="${heavy ? 'bad' : ''}">${money(ev.devCost)}</td></tr>
        <tr><th>착수금 (${Math.round(CONFIG.launchUpfrontRate * 100)}%)</th><td class="${affordable ? '' : 'bad'}">${money(upfront)}</td></tr>
        <tr><th>개발 기간</th><td>${ev.devQuarters}분기 <span class="muted">(현 인력 기준 약 ${realQuarters === Infinity ? '∞' : realQuarters}분기)</span></td></tr>
        <tr><th>인증 기간</th><td>${ev.certQuarters}분기</td></tr>
        <tr><th>필요 인력</th><td>${num(ev.engineersNeeded)}명 <span class="muted">(보유 ${num(s.engineers)}명)</span></td></tr>
        <tr><th>연비 지수</th><td>${ev.efficiency} ${bar(ev.efficiency, 'eff')}</td></tr>
        <tr><th>객실 쾌적성</th><td>${ev.comfort} ${bar(ev.comfort, 'comfort')}</td></tr>
        <tr><th>표준 생산원가</th><td>${money(ev.unitCostBase)}</td></tr>
        <tr><th>정가</th><td>${money(ev.listPrice)}</td></tr>
        <tr><th>결함 위험</th><td class="${ev.defectRisk > 0.25 ? 'bad' : ''}">${(ev.defectRisk * 100).toFixed(1)}%</td></tr>
        <tr><th>라인 건설비</th><td>${money(seg.lineCost)} <span class="muted">(분기 최대 ${seg.lineMaxRate}기)</span></td></tr>
      </table>
      ${heavy ? `<p class="warn-box">총 개발비가 현재 동원 가능한 자금(${money(liquidity)})을 넘는다. 이대로 착수하면 개발 도중 파산할 가능성이 매우 높다.</p>` : ''}
      ${
        spec.derivedFrom && !ev.derivative
          ? `<p class="warn-box">소재·기술·항속을 원형(${esc(spec.derivedFrom.name)})에서 너무 많이 바꿔 형식증명을 물려받을 수 없다. <b>신규 설계 비용</b>으로 계산된다.</p>`
          : ''
      }
      ${ev.derivative ? `<p class="hint">${esc(spec.derivedFrom.name)} 파생형으로 인정 — 개발비·기간 할인이 적용됐다.</p>` : ''}
      <div class="row">
        <input class="name-input" id="design-name" data-action="design-name" placeholder="기종명 (예: DN-200)" maxlength="18" value="${esc(designName || '')}">
        <button class="primary" data-action="launch" ${affordable ? '' : 'disabled'}>개발 착수 · ${money(upfront)}</button>
      </div>
      ${affordable ? '' : '<p class="warn-box">착수금이 부족하다.</p>'}`;
  }

  // ─────────────────────────────── 프로그램 ───────────────────────────────

  function renderPrograms(s) {
    const active = s.programs.filter((p) => p.phase !== 'cancelled');
    if (!active.length) return '<div class="card"><p class="muted">아직 기종이 없다. 설계 탭에서 개발을 착수하라.</p></div>';

    return active
      .map((p) => {
        const eta = E.projectedQuarters(s, p);
        const rows = [];
        rows.push(`<tr><th>세그먼트</th><td>${SEGMENTS[p.segment].name} · ${p.seats}석 · ${num(p.range)}km</td></tr>`);
        rows.push(`<tr><th>연비 / 쾌적성</th><td>${p.efficiency} / ${p.comfort}</td></tr>`);
        rows.push(`<tr><th>정가 / 표준원가</th><td>${money(p.listPrice)} / ${money(p.unitCostBase)}</td></tr>`);
        rows.push(`<tr><th>결함 위험</th><td class="${p.defectRisk > 0.25 ? 'bad' : ''}">${(p.defectRisk * 100).toFixed(1)}%</td></tr>`);
        if (p.phase === 'production') {
          rows.push(`<tr><th>생산 / 인도</th><td>${num(p.produced)}기 / ${num(p.delivered)}기</td></tr>`);
          rows.push(`<tr><th>현재 대당 원가</th><td>${money(D.unitCostAt(p.unitCostBase, p.produced + 1))} <span class="muted">(학습곡선)</span></td></tr>`);
          rows.push(`<tr><th>미인도 재고</th><td>${p.stock}기</td></tr>`);
        }

        let control = '';
        if (p.phase === 'dev') {
          control = `
            <div class="devctl">
              ${bar(p.progress, 'prog')}
              <div class="row between">
                <span>진행 ${p.progress.toFixed(1)}%</span>
                <span class="${eta === Infinity ? 'bad' : ''}">완료까지 ${eta === Infinity ? '동결됨' : eta + '분기'}</span>
              </div>
              <label class="slider">
                <span>인력 배분<b id="lbl-share-${p.id}">${p.share}%${p.share <= 0 ? ' (동결)' : ''}</b></span>
                <input type="range" data-action="share" data-id="${p.id}" min="0" max="100" step="5" value="${p.share}">
              </label>
              <p class="hint">배분 0%는 동결 — 진행도와 개발비 지출이 모두 멈춘다. 필요 인력 ${num(p.engineersNeeded)}명.</p>
              <div class="row">
                <button data-action="quality" data-id="${p.id}" ${p.qualityInvests >= 3 ? 'disabled' : ''}>
                  품질 강화 (${p.qualityInvests}/3) · ${money(p.devCost * 0.06)}
                </button>
                <button class="danger" data-action="cancel-prog" data-id="${p.id}">개발 중단</button>
              </div>
            </div>`;
        } else if (p.phase === 'cert') {
          // 중단 버튼이 없으면 인증 프로그램 3개가 슬롯을 물고 신규 착수가 영영 막힌다.
          control = `<div class="devctl"><p class="cert">형식증명 심사 중 — 잔여 ${p.certRemaining}분기</p>
            <div class="row">
              <button data-action="quality" data-id="${p.id}" ${p.qualityInvests >= 3 ? 'disabled' : ''}>품질 강화 (${p.qualityInvests}/3) · ${money(p.devCost * 0.06)}</button>
              <button class="danger" data-action="cancel-prog" data-id="${p.id}">개발 중단</button>
            </div></div>`;
        } else if (p.phase === 'production') {
          const ordered = orderedBy(s, p.id);
          const lines = s.lines.filter((l) => l.programId === p.id).length;
          control = `<div class="devctl">
            <p>수주 잔고 <b>${ordered}기</b> · 전용 라인 <b>${lines}개</b></p>
            ${ordered > 0 && lines === 0 ? '<p class="warn-box">라인이 없어 인도할 수 없다. 생산 탭에서 라인을 세워라.</p>' : ''}
          </div>`;
        }

        return `<div class="card program ${p.phase}">
          <div class="row between">
            <h3>${esc(p.name)} ${p.legacy ? '<span class="tag">노후 주력기</span>' : ''} ${p.derivative ? '<span class="tag">파생형</span>' : ''}</h3>
            <span class="phase ${p.phase}">${phaseLabel(p)}</span>
          </div>
          <table class="spec">${rows.join('')}</table>
          ${control}
        </div>`;
      })
      .join('');
  }

  // ─────────────────────────────── 생산 ───────────────────────────────

  function renderProduction(s) {
    const ready = s.programs.filter((p) => p.phase === 'production');
    if (!ready.length) return '<div class="card"><p class="muted">양산 가능한 기종이 없다.</p></div>';

    const buildButtons = ready
      .map((p) => {
        const seg = SEGMENTS[p.segment];
        const can = s.cash >= seg.lineCost;
        return `<button data-action="build-line" data-id="${p.id}" ${can ? '' : 'disabled'}>
          ${esc(p.name)} 라인 신설 · ${money(seg.lineCost)}
        </button>`;
      })
      .join('');

    const lines = s.lines.length
      ? s.lines
          .map((l) => {
            const p = s.programs.find((x) => x.id === l.programId);
            const ordered = orderedBy(s, l.programId);
            return `<div class="line ${l.idle ? 'idle' : ''}">
              <div class="row between">
                <b>${esc(p ? p.name : '?')}</b>
                <span class="${l.idle ? 'bad' : 'good'}">${l.idle ? '가동 중지' : '가동 중'}</span>
              </div>
              <div class="row between"><span>가동률</span><span>${Math.round(l.ramp * 100)}%</span></div>
              ${bar(l.ramp * 100, 'ramp')}
              <p class="muted">분기 최대 ${l.capacity}기 · 이 기종 잔고 ${ordered}기</p>
              <div class="row">
                <button data-action="toggle-line" data-id="${l.id}">${l.idle ? '가동 재개' : '가동 중지'}</button>
                <button class="danger" data-action="close-line" data-id="${l.id}">폐쇄</button>
              </div>
            </div>`;
          })
          .join('')
      : '<p class="muted">가동 중인 라인이 없다.</p>';

    const stocks = ready
      .filter((p) => p.stock > 0)
      .map((p) => {
        const grounded = (s.effects.grounded && s.effects.grounded[p.id]) || 0;
        return `<div class="row between stockrow">
          <span><b>${esc(p.name)}</b> 미인도 재고 ${p.stock}기${grounded ? ` <span class="bad">· 운항 정지 ${grounded}분기</span>` : ''}</span>
          <button data-action="sell-stock" data-id="${p.id}" ${grounded ? 'disabled' : ''}>
            ${grounded ? '정지 중 처분 불가' : `정가 68%로 처분 · ${money(p.stock * p.listPrice * 0.68)}`}
          </button>
        </div>`;
      })
      .join('');

    return `
      <section class="card"><h3>라인 신설</h3>
        <p class="muted">라인은 주문 잔고 범위 안에서만 생산한다. 주문이 없으면 가동률이 서서히 떨어진다.</p>
        <div class="row wrap">${buildButtons}</div>
      </section>
      <section class="card"><h3>조립 라인</h3><div class="lines">${lines}</div></section>
      ${stocks ? `<section class="card"><h3>미인도 재고 (화이트테일)</h3><p class="muted">주문 취소 등으로 남은 기체. 오래 쥐고 있으면 유지비가 나간다.</p>${stocks}</section>` : ''}`;
  }

  // ─────────────────────────────── 수주 ───────────────────────────────

  function renderRfps(s, discountDraft) {
    if (!s.rfps.length) return '<div class="card"><p class="muted">이번 분기에는 새 입찰 공고가 없다.</p></div>';

    return s.rfps
      .map((rfp) => {
        const bid = s.bids[rfp.id];
        const candidates = s.programs.filter((p) => p.phase === 'production' && p.segment === rfp.segment);
        const discount = bid ? bid.discount : (discountDraft && discountDraft[rfp.id]) ?? 0.1;
        const scored = candidates.map((p) => ({ p, sc: B.scoreBid(s, rfp, p, discount) }));
        const anyBiddable = scored.some((x) => !x.sc.blocked);

        const options = renderBidCandidates(s, rfp, discount);

        return `<div class="card rfp">
          <div class="row between">
            <h3>${esc(rfp.airlineName)} <span class="muted">${esc(rfp.home)}</span></h3>
            <span class="qty">${rfp.qty}기</span>
          </div>
          <table class="spec">
            <tr><th>요구 기종</th><td>${rfp.segmentName} · ${rfp.reqSeats}석급 · ${num(rfp.reqRange)}km</td></tr>
            <tr><th>가격 민감도</th><td>${rfp.priceSensitivity >= 1.2 ? '매우 높음' : rfp.priceSensitivity >= 1.0 ? '높음' : rfp.priceSensitivity >= 0.8 ? '보통' : '낮음 (프리미엄 중시)'}</td></tr>
            <tr><th>경쟁 강도</th><td>${rfp.rivalHint.label}</td></tr>
            <tr><th>맞붙을 기종</th><td>${esc(rfp.rivalHint.rival || '—')}</td></tr>
            <tr><th>우리와의 관계</th><td>${Math.round(s.relations[rfp.airlineId] ?? 40)} / 100</td></tr>
          </table>
          ${
            !candidates.length
              ? '<p class="muted">이 세그먼트에 양산 중인 기종이 없다.</p>'
              : !anyBiddable
                ? `<div class="cands" id="cands-${rfp.id}">${options}</div>
                   <p class="warn-box">보유 기종 중 이 공고의 요구를 만족하는 기체가 없다. 후속기 개발이 급하다.</p>`
                : `<div class="cands" id="cands-${rfp.id}">${options}</div>
                   <label class="slider">
                     <span>할인율<b id="disc-label-${rfp.id}">${Math.round(discount * 100)}%</b></span>
                     <input type="range" data-action="discount" data-rfp="${rfp.id}" min="0" max="${CONFIG.maxDiscount * 100}" step="1" value="${Math.round(discount * 100)}">
                   </label>
                   <div id="bidinfo-${rfp.id}">${renderBidInfo(s, rfp)}</div>`
          }
        </div>`;
      })
      .join('');
  }

  /** 후보 기종 버튼 — 표시 점수·가격이 현재 할인율을 반영해야 한다. */
  function renderBidCandidates(s, rfp, discount) {
    const bid = s.bids[rfp.id];
    return s.programs
      .filter((p) => p.phase === 'production' && p.segment === rfp.segment)
      .map((p) => {
        const sc = B.scoreBid(s, rfp, p, discount);
        const sel = bid && bid.programId === p.id;
        return `<button class="cand ${sel ? 'on' : ''} ${sc.blocked ? 'blocked' : ''}"
                  data-action="pick-bid" data-rfp="${rfp.id}" data-id="${p.id}" ${sc.blocked ? 'disabled' : ''}>
                  <b>${esc(p.name)}</b>
                  <span>${sc.blocked ? sc.blocked : `점수 ${sc.total} · 대당 ${money(sc.price)}`}</span>
                </button>`;
      })
      .join('');
  }

  /** 선택한 기종 + 할인율에 대한 입찰 요약 — 슬라이더 조작 시 이 영역만 갱신한다. */
  function renderBidInfo(s, rfp) {
    const bid = s.bids[rfp.id];
    if (!bid) return '<p class="muted">입찰할 기종을 선택하라. 선택하지 않으면 이번 공고는 포기한다.</p>';
    const p = s.programs.find((x) => x.id === bid.programId);
    if (!p) return '';
    const sc = B.scoreBid(s, rfp, p, bid.discount);
    if (sc.blocked) return `<p class="warn-box">${sc.blocked} — 이 기종으로는 입찰할 수 없다.</p>`;

    const unitCost = D.unitCostAt(p.unitCostBase, p.produced + 1);
    const margin = sc.price - unitCost;
    const total = sc.price * rfp.qty;

    return `<table class="spec">
        <tr><th>입찰 점수</th><td><b>${sc.total}</b> ${bar(sc.total, 'score')}</td></tr>
        <tr><th>대당 가격</th><td>${money(sc.price)} <span class="muted">(정가 ${money(p.listPrice)})</span></td></tr>
        <tr><th>현재 대당 원가</th><td>${money(unitCost)}</td></tr>
        <tr><th>대당 마진</th><td class="${margin >= 0 ? 'good' : 'bad'}">${margin >= 0 ? '+' : ''}${money(margin)}</td></tr>
        <tr><th>전량 수주 시</th><td>${money(total)} <span class="muted">(선수금 ${money(total * CONFIG.depositRate)})</span></td></tr>
      </table>
      <div class="parts">
        ${part('제원 적합', sc.parts.spec)}${part('가격', sc.parts.price)}${part('연비', sc.parts.eff)}
        ${part('객실', sc.parts.comfort)}${part('평판', sc.parts.rep)}${part('관계', sc.parts.rel)}
      </div>
      <div class="row"><button class="ghost" data-action="withdraw" data-rfp="${rfp.id}">입찰 포기</button></div>`;
  }

  function part(label, v) {
    return `<div class="part"><span>${label}</span>${bar(v, 'part')}<b>${v}</b></div>`;
  }

  // ─────────────────────────────── 재무 ───────────────────────────────

  function renderFinance(s) {
    const rows = s.history
      .slice(-16)
      .reverse()
      .map(
        (h) => `<tr>
          <td>${h.label}</td>
          <td>${money(h.revenue)}</td>
          <td>${money(h.cost)}</td>
          <td class="${h.net >= 0 ? 'good' : 'bad'}">${h.net >= 0 ? '+' : ''}${money(h.net)}</td>
          <td>${h.delivered}기</td>
          <td>${money(h.cash)}</td>
          <td>${money(h.debt)}</td>
        </tr>`,
      )
      .join('');

    const room = CONFIG.maxDebt - s.debt;
    return `
      <section class="grid2">
        <div class="card">
          <h3>차입 / 상환</h3>
          <p class="muted">분기 이자율 ${(CONFIG.interestPerQuarter * 100).toFixed(1)}%${s.effects.rateBumpQuarters > 0 ? ` <b class="bad">(+${(s.effects.rateBump * 100).toFixed(1)}%p 신용경색)</b>` : ''} · 한도 ${money(CONFIG.maxDebt)} · 여유 ${money(room)}</p>
          <div class="row wrap">
            <button data-action="borrow" data-amt="1000" ${room <= 0 ? 'disabled' : ''}>${money(Math.min(1000, room))} 차입</button>
            <button data-action="borrow" data-amt="3000" ${room <= 0 ? 'disabled' : ''}>${money(Math.min(3000, room))} 차입</button>
            <button data-action="repay" data-amt="1000" ${s.cash < 1 || s.debt < 1 ? 'disabled' : ''}>${money(Math.min(1000, s.cash, s.debt))} 상환</button>
            <button data-action="repay" data-amt="3000" ${s.cash < 1 || s.debt < 1 ? 'disabled' : ''}>${money(Math.min(3000, s.cash, s.debt))} 상환</button>
          </div>
          <p class="hint">분기 이자 ${money(s.debt * (CONFIG.interestPerQuarter + (s.effects.rateBumpQuarters > 0 ? s.effects.rateBump : 0)))}</p>
        </div>
        <div class="card">
          <h3>인력</h3>
          <p class="muted">엔지니어 ${num(s.engineers)}명 · 분기 인건비 ${money(s.engineers * CONFIG.engineerCostPerQuarter)}</p>
          <div class="row wrap">
            <button data-action="hire" data-amt="500">500명 채용 · ${money(500 * CONFIG.engineerHireCost)}</button>
            <button data-action="hire" data-amt="1500">1,500명 채용 · ${money(1500 * CONFIG.engineerHireCost)}</button>
            <button class="danger" data-action="hire" data-amt="-500">500명 감원</button>
          </div>
          <p class="hint">인력이 많을수록 개발이 빨라지지만 분기 인건비가 계속 나간다.</p>
        </div>
      </section>
      <section class="card">
        <h3>분기 실적</h3>
        ${
          rows
            ? `<table class="hist"><thead><tr><th>분기</th><th>매출</th><th>비용</th><th>손익</th><th>인도</th><th>현금</th><th>부채</th></tr></thead><tbody>${rows}</tbody></table>`
            : '<p class="muted">아직 정산된 분기가 없다.</p>'
        }
      </section>`;
  }

  // ─────────────────────────────── 기록 ───────────────────────────────

  function renderLog(s) {
    return `<section class="card"><h3>경영 기록</h3><ul class="log">${s.log.map(logItem).join('')}</ul></section>`;
  }

  root.AirlinerPanels = {
    renderOverview,
    renderDesign,
    renderDesignPreview,
    renderPrograms,
    renderProduction,
    renderRfps,
    renderBidInfo,
    renderBidCandidates,
    renderFinance,
    renderLog,
    esc,
    money,
    num,
  };
})(typeof globalThis !== 'undefined' ? globalThis : this);
