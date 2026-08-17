/*
 * 앱 컨트롤러 — HUD·탭·이벤트 위임·저장/불러오기.
 *
 * 모든 클릭/입력은 document 한 곳에서 data-action으로 위임 처리한다.
 * 패널을 통째로 다시 그려도 핸들러가 살아있고, 배선이 한 군데에 모인다.
 */
(function (root) {
  'use strict';

  const E = root.AirlinerEngine;
  const D = root.AirlinerDesign;
  const P = root.AirlinerPanels;
  const { CONFIG, SEGMENTS } = root.AirlinerData;

  const SAVE_KEY = 'airliner.save.v1';
  const money = E.fmtMoney;

  const ui = {
    state: null,
    tab: 'overview',
    spec: D.defaultSpec('narrow'),
    // 기종을 고르기 전에 만진 할인율 (RFP별). 선택 시 이 값을 그대로 물려준다.
    discountDraft: {},
  };

  const TABS = [
    { id: 'overview', name: '개요' },
    { id: 'design', name: '설계' },
    { id: 'programs', name: '프로그램' },
    { id: 'production', name: '생산' },
    { id: 'rfps', name: '수주' },
    { id: 'finance', name: '재무' },
    { id: 'log', name: '기록' },
  ];

  // ─────────────────────────────── 렌더 ───────────────────────────────

  function render() {
    const s = ui.state;
    renderHud(s);
    renderTabs(s);

    const panel = document.getElementById('panel');
    switch (ui.tab) {
      case 'design':
        panel.innerHTML = P.renderDesign(s, ui.spec);
        break;
      case 'programs':
        panel.innerHTML = P.renderPrograms(s);
        break;
      case 'production':
        panel.innerHTML = P.renderProduction(s);
        break;
      case 'rfps':
        panel.innerHTML = P.renderRfps(s, ui.discountDraft);
        break;
      case 'finance':
        panel.innerHTML = P.renderFinance(s);
        break;
      case 'log':
        panel.innerHTML = P.renderLog(s);
        break;
      default:
        panel.innerHTML = P.renderOverview(s);
    }
    save();
  }

  function renderHud(s) {
    const share = (E.marketShare(s) * 100).toFixed(1);
    const fuel = s.market.fuelIndex;
    const demand = s.market.demandIndex;
    const trend = (v) => (v >= 1.25 ? 'up' : v <= 0.8 ? 'down' : '');

    document.getElementById('hud').innerHTML = `
      <div class="hud-left">
        <div class="hud-company">${P.esc(s.company)}</div>
        <div class="hud-date">${E.turnLabel(s.turn)} <span class="muted">· ${CONFIG.totalTurns - s.turn}분기 남음</span></div>
      </div>
      <div class="hud-stats">
        ${hudStat('현금', money(s.cash), s.cash < 500 ? 'bad' : '')}
        ${hudStat('부채', money(s.debt), s.debt >= CONFIG.maxDebt * 0.9 ? 'bad' : '')}
        ${hudStat('수주 잔고', P.num(E.totalBacklog(s)) + '기')}
        ${hudStat('점유율', share + '%')}
        ${hudStat('평판', Math.round(s.reputation))}
        ${hudStat('연료지수', fuel.toFixed(2), trend(fuel))}
        ${hudStat('수요지수', demand.toFixed(2), trend(demand) === 'up' ? 'good' : trend(demand) === 'down' ? 'bad' : '')}
      </div>
      <button class="next" data-action="next-turn">분기 종료 ▸</button>`;
  }

  function hudStat(label, value, tone) {
    return `<div class="hud-stat ${tone || ''}"><span>${label}</span><b>${value}</b></div>`;
  }

  function renderTabs(s) {
    document.getElementById('tabs').innerHTML = TABS.map((t) => {
      let badge = '';
      if (t.id === 'rfps' && s.rfps.length) badge = `<i>${s.rfps.length}</i>`;
      return `<button class="tab ${t.id === ui.tab ? 'on' : ''}" data-action="tab" data-tab="${t.id}">${t.name}${badge}</button>`;
    }).join('');
  }

  function toast(text, tone) {
    const el = document.getElementById('toast');
    el.className = 'toast show ' + (tone || '');
    el.innerHTML = text;
    clearTimeout(toast._t);
    toast._t = setTimeout(() => (el.className = 'toast'), 4200);
  }

  // ─────────────────────────────── 행동 처리 ───────────────────────────────

  /** engine 호출 결과를 그대로 토스트로 흘려보내는 공통 래퍼 */
  function act(result, okMsg) {
    if (!result) return false;
    if (!result.ok) {
      toast(result.error, 'bad');
      return false;
    }
    if (okMsg) toast(okMsg, 'good');
    render();
    return true;
  }

  // 게임 종료 뒤에도 허용되는 행동 — 나머지는 저장 상태를 바꿔 최종 성적과 어긋나게 만든다.
  const ALLOWED_AFTER_END = new Set(['tab', 'new-game', 'close-modal']);

  function onClick(ev) {
    const btn = ev.target.closest('[data-action]');
    if (!btn || btn.disabled) return;
    const s = ui.state;
    const a = btn.dataset.action;

    if (s.gameOver && !ALLOWED_AFTER_END.has(a)) {
      toast('경영이 종료되어 더 이상 조작할 수 없다. 새 게임을 시작하라.', 'bad');
      return;
    }

    switch (a) {
      case 'tab':
        ui.tab = btn.dataset.tab;
        render();
        break;

      case 'next-turn':
        nextTurn();
        break;

      case 'design-seg':
        ui.spec = D.defaultSpec(btn.dataset.seg);
        render();
        break;

      case 'design-mat':
        ui.spec.material = btn.dataset.mat;
        render();
        break;

      case 'derive': {
        const base = s.programs.find((p) => p.id === btn.dataset.id);
        if (base) {
          ui.spec = E.derivativeSpec(base, 20);
          ui.tab = 'design';
          toast(`${P.esc(base.name)} 파생형 설계를 불러왔다. 좌석수를 조정해 보라.`);
          render();
        }
        break;
      }

      case 'launch': {
        const nameEl = document.getElementById('design-name');
        const name = nameEl && nameEl.value.trim();
        const r = E.launchProgram(s, ui.spec, name);
        if (act(r)) {
          ui.tab = 'programs';
          toast(`${P.esc(r.program.name)} 개발에 착수했다.`, 'good');
          render();
        }
        break;
      }

      case 'quality':
        act(E.investQuality(s, btn.dataset.id));
        break;

      case 'cancel-prog': {
        const p = s.programs.find((x) => x.id === btn.dataset.id);
        if (p && confirm(`${p.name} 개발을 중단하시겠습니까?\n투입된 ${money(p.spent)}는 회수되지 않고 평판도 떨어집니다.`)) {
          act(E.cancelProgram(s, btn.dataset.id));
        }
        break;
      }

      case 'build-line':
        act(E.buildLine(s, btn.dataset.id));
        break;

      case 'toggle-line':
        act(E.toggleLine(s, btn.dataset.id));
        break;

      case 'close-line': {
        if (confirm('이 라인을 폐쇄하시겠습니까? 건설비의 20%만 회수됩니다.')) {
          act(E.closeLine(s, btn.dataset.id));
        }
        break;
      }

      case 'sell-stock': {
        const p = s.programs.find((x) => x.id === btn.dataset.id);
        if (p) act(E.sellStock(s, p.id, p.stock));
        break;
      }

      case 'pick-bid': {
        const rfpId = btn.dataset.rfp;
        const cur = s.bids[rfpId];
        const discount = cur ? cur.discount : ui.discountDraft[rfpId] ?? 0.1;
        E.setBid(s, rfpId, btn.dataset.id, discount);
        render();
        break;
      }

      case 'withdraw':
        E.setBid(s, btn.dataset.rfp, null);
        render();
        break;

      case 'borrow':
        act(E.borrow(s, Number(btn.dataset.amt)));
        break;

      case 'repay':
        act(E.repay(s, Number(btn.dataset.amt)));
        break;

      case 'hire':
        act(E.hireEngineers(s, Number(btn.dataset.amt)));
        break;

      case 'new-game':
        if (confirm('새 게임을 시작하시겠습니까? 현재 진행 상황은 사라집니다.')) startNewGame();
        break;

      case 'close-modal':
        closeModal();
        break;
    }
  }

  /**
   * 슬라이더 입력 — 드래그 중에 패널을 통째로 다시 그리면 드래그가 끊기므로,
   * 라벨과 해당 미리보기 영역만 직접 갱신한다.
   */
  function onInput(ev) {
    const el = ev.target.closest('[data-action]');
    if (!el) return;
    const s = ui.state;

    if (el.dataset.action === 'design-input') {
      const key = el.dataset.key;
      ui.spec[key] = Number(el.value);
      const unit = key === 'seats' ? '석' : key === 'range' ? 'km' : '';
      const lbl = document.getElementById('lbl-' + key);
      if (lbl) lbl.textContent = P.num(ui.spec[key]) + unit;
      const prev = document.getElementById('design-preview');
      if (prev) prev.innerHTML = P.renderDesignPreview(s, ui.spec);
    } else if (el.dataset.action === 'share') {
      const p = s.programs.find((x) => x.id === el.dataset.id);
      if (!p) return;
      p.share = Number(el.value);
      const lbl = document.getElementById('lbl-share-' + p.id);
      if (lbl) lbl.textContent = p.share + '%' + (p.share <= 0 ? ' (동결)' : '');
    } else if (el.dataset.action === 'discount') {
      const rfpId = el.dataset.rfp;
      const bid = s.bids[rfpId];
      const pct = Number(el.value);
      const lbl = document.getElementById('disc-label-' + rfpId);
      if (lbl) lbl.textContent = pct + '%';
      ui.discountDraft[rfpId] = pct / 100; // 기종 미선택 상태에서도 값을 기억한다
      if (bid) {
        E.setBid(s, rfpId, bid.programId, pct / 100);
        const info = document.getElementById('bidinfo-' + rfpId);
        const rfp = s.rfps.find((r) => r.id === rfpId);
        if (info && rfp) info.innerHTML = P.renderBidInfo(s, rfp);
      }
    }
  }

  /** 모달이 열려 있으면 Esc 로 닫고, Tab 포커스를 모달 안에 가둔다. */
  function onKeydown(ev) {
    const modal = document.getElementById('modal');
    if (!modal || !modal.classList.contains('show')) return;
    if (ev.key === 'Escape') {
      closeModal();
      return;
    }
    if (ev.key !== 'Tab') return;
    const focusables = modal.querySelectorAll('button, [href], input, select, textarea, [tabindex]:not([tabindex="-1"])');
    if (!focusables.length) return;
    const first = focusables[0];
    const last = focusables[focusables.length - 1];
    if (ev.shiftKey && document.activeElement === first) {
      ev.preventDefault();
      last.focus();
    } else if (!ev.shiftKey && document.activeElement === last) {
      ev.preventDefault();
      first.focus();
    }
  }

  /** 슬라이더에서 손을 뗐을 때 파생 표시(완료 예상 등)를 최신화한다. */
  function onChange(ev) {
    const el = ev.target.closest('[data-action]');
    if (!el) return;
    if (el.dataset.action === 'share') render();
  }

  // ─────────────────────────────── 턴 진행 ───────────────────────────────

  function nextTurn() {
    const s = ui.state;
    const unbid = s.rfps.filter((r) => !s.bids[r.id]).length;
    if (unbid && s.rfps.length && ui.tab !== 'rfps') {
      if (!confirm(`입찰하지 않은 공고가 ${unbid}건 있습니다. 그대로 분기를 종료할까요?`)) {
        ui.tab = 'rfps';
        render();
        return;
      }
    }

    const r = E.endTurn(s);
    if (!r.ok) {
      toast(r.error, 'bad');
      return;
    }

    const rep = r.report;
    const net = rep.revenue - rep.productionCost - rep.rdCost - rep.overhead - rep.interest;
    toast(
      `${rep.label} 정산 — 매출 ${money(rep.revenue)} · 인도 ${rep.delivered}기 · 손익 <b>${net >= 0 ? '+' : ''}${money(net)}</b>`,
      net >= 0 ? 'good' : 'bad',
    );

    ui.tab = 'overview';
    render();
    if (s.gameOver) showGameOver(s);
  }

  function showGameOver(s) {
    const g = s.gameOver;
    const bankrupt = g.reason === 'bankrupt';
    const body = `
      <h2 id="modal-title">${bankrupt ? '파산' : '20년의 경영이 끝났다'}</h2>
      <p class="go-reason">${
        bankrupt
          ? `${E.turnLabel(s.turn)}, 자금이 고갈되고 차입 한도까지 소진됐다. 회사는 법정관리에 들어간다.`
          : `${s.company}는 ${P.num(g.delivered)}기의 여객기를 세상에 내보냈다.`
      }</p>
      <div class="grade ${g.grade}">${g.grade}</div>
      <table class="spec">
        <tr><th>최종 점수</th><td>${P.num(g.score)}</td></tr>
        <tr><th>누적 인도</th><td>${P.num(g.delivered)}기</td></tr>
        <tr><th>시장 점유율</th><td>${(g.share * 100).toFixed(1)}%</td></tr>
        <tr><th>순자산</th><td>${money(g.worth)}</td></tr>
        <tr><th>최종 평판</th><td>${Math.round(s.reputation)} / 100</td></tr>
      </table>
      <div class="row">
        <button class="primary" data-action="new-game">새 게임</button>
        <button class="ghost" data-action="close-modal">기록 살펴보기</button>
      </div>`;
    openModal(body);
  }

  /**
   * 모달 열기/닫기 — 스크린리더가 대화상자를 인지하고 키보드 포커스가
   * 뒤쪽 '분기 종료' 버튼에 남지 않도록 포커스를 옮기고 되돌린다.
   */
  let lastFocused = null;

  function openModal(bodyHtml) {
    const modal = document.getElementById('modal');
    modal.querySelector('.modal-body').innerHTML = bodyHtml;
    modal.classList.add('show');
    modal.setAttribute('aria-hidden', 'false');
    lastFocused = document.activeElement;
    const first = modal.querySelector('button');
    if (first) first.focus();
  }

  function closeModal() {
    const modal = document.getElementById('modal');
    modal.classList.remove('show');
    modal.setAttribute('aria-hidden', 'true');
    if (lastFocused && lastFocused.isConnected) lastFocused.focus();
    lastFocused = null;
  }

  // ─────────────────────────────── 저장 / 시작 ───────────────────────────────

  function save() {
    try {
      localStorage.setItem(SAVE_KEY, JSON.stringify(ui.state));
    } catch (e) {
      /* 저장 실패는 게임 진행을 막지 않는다 (시크릿 모드 등) */
    }
  }

  function load() {
    try {
      const raw = localStorage.getItem(SAVE_KEY);
      if (!raw) return null;
      const s = JSON.parse(raw);
      // 스키마가 바뀐 옛 세이브는 조용히 버린다.
      if (!s || s.version !== 1 || !Array.isArray(s.programs)) return null;
      return s;
    } catch (e) {
      return null;
    }
  }

  /**
   * 새 게임의 출발 시드.
   * 게임 진행 자체는 rng.js의 시드 고정 PRNG가 굴리고 이 값은 그 출발점일 뿐이지만,
   * 시드가 PRNG를 타고 전체 상태로 퍼지는 흐름을 코드 스캐너가 "취약한 난수"로 잡는다.
   * 게임 시드에 암호학적 강도가 필요하진 않아도 crypto를 쓰는 데 드는 비용이 없으므로
   * 경고를 남겨두는 대신 그냥 crypto로 뽑는다.
   */
  function randomSeed() {
    const c = typeof crypto !== 'undefined' ? crypto : null;
    if (c && typeof c.getRandomValues === 'function') {
      return c.getRandomValues(new Uint32Array(1))[0] >>> 0;
    }
    // crypto가 없는 아주 오래된 브라우저 대비 (시드 품질만 떨어질 뿐 동작은 같다).
    const t = Date.now();
    return (t ^ (t << 13) ^ (t >>> 7)) >>> 0;
  }

  function startNewGame() {
    const seed = randomSeed();
    ui.state = E.newGame(seed);
    ui.tab = 'overview';
    ui.spec = D.defaultSpec('narrow');
    ui.discountDraft = {};
    closeModal();
    render();
    toast('새 경영을 시작한다. 주력기 DN-150이 버텨주는 동안 후속기를 띄워라.', 'good');
  }

  function boot() {
    const saved = load();
    ui.state = saved || E.newGame(randomSeed());
    document.addEventListener('click', onClick);
    document.addEventListener('input', onInput);
    document.addEventListener('change', onChange);
    document.addEventListener('keydown', onKeydown);
    render();
    if (ui.state.gameOver) showGameOver(ui.state);
    else if (saved) toast('저장된 경영을 이어서 진행한다.');
  }

  root.AirlinerUI = { boot, ui };
  if (typeof document !== 'undefined') {
    if (document.readyState === 'loading') document.addEventListener('DOMContentLoaded', boot);
    else boot();
  }
})(typeof globalThis !== 'undefined' ? globalThis : this);
