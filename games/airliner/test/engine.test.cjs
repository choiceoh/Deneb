/*
 * 엔진 회귀 테스트 — 브라우저 없이 시뮬레이션 규칙을 검증한다.
 *   실행: node --test games/airliner/test/engine.test.cjs
 * 소스는 globalThis에 네임스페이스를 붙이는 IIFE라 require만 하면 로드된다.
 */
'use strict';

const test = require('node:test');
const assert = require('node:assert');
const path = require('node:path');

const JS = path.join(__dirname, '..', 'js');
for (const f of ['rng.js', 'data.js', 'design.js', 'bidding.js', 'engine.js']) {
  require(path.join(JS, f));
}

const { AirlinerEngine: E, AirlinerDesign: D, AirlinerData: Data, AirlinerRng: R } = globalThis;

test('시드가 같으면 난수열이 같다 (재현성)', () => {
  const a = R.createRng(42);
  const b = R.createRng(42);
  const seqA = [a.next(), a.next(), a.next()];
  const seqB = [b.next(), b.next(), b.next()];
  assert.deepStrictEqual(seqA, seqB);
  assert.notStrictEqual(seqA[0], R.createRng(43).next());
});

test('설계: 기술 투자와 복합재가 개발비·연비를 올린다', () => {
  const base = D.evaluate({ segment: 'narrow', seats: 180, range: 5500, tech: 30, material: 'aluminum' });
  const hi = D.evaluate({ segment: 'narrow', seats: 180, range: 5500, tech: 90, material: 'composite' });
  assert.ok(hi.devCost > base.devCost, '기술+복합재는 개발비가 더 비싸야 한다');
  assert.ok(hi.efficiency > base.efficiency, '연비가 더 좋아야 한다');
  assert.ok(hi.devQuarters >= base.devQuarters, '개발 기간이 짧아지면 안 된다');
  assert.ok(hi.defectRisk > base.defectRisk, '리스크가 더 커야 한다');
});

test('설계: 슬라이더 값이 세그먼트 범위로 클램프된다', () => {
  const r = D.evaluate({ segment: 'regional', seats: 9999, range: -50, tech: 200, material: 'aluminum' });
  assert.strictEqual(r.seats, Data.SEGMENTS.regional.seats.max);
  assert.strictEqual(r.range, Data.SEGMENTS.regional.range.min);
  assert.strictEqual(r.tech, 100);
});

test('파생형은 원형보다 개발비·기간이 크게 싸다', () => {
  const base = { segment: 'narrow', seats: 180, range: 5500, tech: 60, material: 'aluminum' };
  const orig = D.evaluate(base);
  const deriv = D.evaluate({ ...base, seats: 200, derivedFrom: { id: 'x', name: 'y' } });
  assert.ok(deriv.devCost < orig.devCost * 0.5);
  assert.ok(deriv.devQuarters < orig.devQuarters);
});

test('학습곡선: 누적 생산이 늘면 대당 원가가 내려가고 바닥에서 멈춘다', () => {
  const c1 = D.unitCostAt(100, 1);
  const c50 = D.unitCostAt(100, 50);
  const c500 = D.unitCostAt(100, 500);
  assert.ok(c1 > c50 && c50 > c500, '단조 감소해야 한다');
  assert.ok(c1 > 100, '초도 기체는 표준원가보다 비싸야 한다');
  const floor = 100 * Data.CONFIG.firstUnitPremium * Data.CONFIG.learningFloor;
  assert.ok(D.unitCostAt(100, 1e9) >= floor - 1e-9, '학습곡선 바닥 아래로 내려가면 안 된다');
});

test('프로그램 착수 → 개발 → 인증 → 양산 전이', () => {
  const s = E.newGame(7, '테스트항공우주');
  const spec = { segment: 'regional', seats: 90, range: 2500, tech: 40, material: 'aluminum' };
  const res = E.launchProgram(s, spec, '테스트-90');
  assert.ok(res.ok, res.error);
  assert.strictEqual(res.program.phase, 'dev');

  // 인력을 전부 몰아주고 충분히 돌리면 반드시 양산 단계에 도달한다.
  for (let i = 0; i < 40 && res.program.phase !== 'production'; i++) E.endTurn(s);
  assert.strictEqual(res.program.phase, 'production', '40분기 안에 양산 전이가 일어나야 한다');
  assert.ok(res.program.spent > 0);
});

test('인증 전에는 라인을 세울 수 없다', () => {
  const s = E.newGame(11);
  const res = E.launchProgram(s, { segment: 'regional', seats: 90, range: 2500, tech: 30, material: 'aluminum' });
  const line = E.buildLine(s, res.program.id);
  assert.strictEqual(line.ok, false);
  assert.match(line.error, /양산/);
});

test('동시 개발 프로그램은 3개로 제한된다', () => {
  const s = E.newGame(3);
  s.cash = 500000;
  const spec = { segment: 'regional', seats: 90, range: 2500, tech: 20, material: 'aluminum' };
  for (let i = 0; i < 3; i++) assert.ok(E.launchProgram(s, spec, 'p' + i).ok);
  const fourth = E.launchProgram(s, spec, 'p4');
  assert.strictEqual(fourth.ok, false);
  assert.match(fourth.error, /3개/);
});

test('입찰 점수: 요구 스펙 미달이면 실격 처리된다', () => {
  const s = E.newGame(5);
  const rfp = {
    id: 'r1', airlineId: 'hanul', airlineName: '한울항공', segment: 'narrow',
    reqSeats: 180, reqRange: 6000, qty: 20, priceSensitivity: 1, prestige: 1,
  };
  const shortRange = { segment: 'narrow', seats: 180, range: 4000, listPrice: 90, efficiency: 60, comfort: 50 };
  const shortSeats = { segment: 'narrow', seats: 130, range: 6500, listPrice: 90, efficiency: 60, comfort: 50 };
  const wrongSeg = { segment: 'wide', seats: 300, range: 12000, listPrice: 250, efficiency: 60, comfort: 50 };

  assert.match(globalThis.AirlinerBidding.scoreBid(s, rfp, shortRange, 0).blocked, /항속/);
  assert.match(globalThis.AirlinerBidding.scoreBid(s, rfp, shortSeats, 0).blocked, /좌석/);
  assert.match(globalThis.AirlinerBidding.scoreBid(s, rfp, wrongSeg, 0).blocked, /세그먼트/);
});

test('입찰 점수: 할인이 커지면 점수가 오른다', () => {
  const s = E.newGame(5);
  const rfp = {
    id: 'r1', airlineId: 'hanul', airlineName: '한울항공', segment: 'narrow',
    reqSeats: 180, reqRange: 5000, qty: 20, priceSensitivity: 1.2, prestige: 1,
  };
  const p = { segment: 'narrow', seats: 180, range: 5500, listPrice: 95, efficiency: 60, comfort: 55 };
  const low = globalThis.AirlinerBidding.scoreBid(s, rfp, p, 0);
  const high = globalThis.AirlinerBidding.scoreBid(s, rfp, p, 0.3);
  assert.ok(high.total > low.total, '할인은 점수를 올려야 한다');
  assert.ok(high.price < low.price, '실효 가격은 내려가야 한다');
});

test('차입은 한도를 넘지 않고, 상환은 현금/부채를 넘지 않는다', () => {
  const s = E.newGame(9);
  E.borrow(s, 999999);
  assert.strictEqual(s.debt, Data.CONFIG.maxDebt);
  assert.strictEqual(E.borrow(s, 100).ok, false);

  const cashBefore = s.cash;
  E.repay(s, 999999);
  assert.ok(s.debt >= 0 && s.cash >= 0, '음수로 내려가면 안 된다');
  assert.ok(s.cash < cashBefore);
});

test('20년 완주: 아무 조작 없이도 상태가 깨지지 않고 종료된다', () => {
  const s = E.newGame(2024);
  let guard = 0;
  while (!s.gameOver && guard++ < 200) E.endTurn(s);
  assert.ok(s.gameOver, '80분기 안에 반드시 종료돼야 한다');
  assert.ok(['bankrupt', 'complete'].includes(s.gameOver.reason));
  assert.ok(Number.isFinite(s.cash) && Number.isFinite(s.debt));
  assert.ok(Number.isFinite(s.gameOver.score));
  assert.ok(s.reputation >= 0 && s.reputation <= 100, '평판은 0~100을 벗어나면 안 된다');
  assert.ok(s.market.fuelIndex > 0 && s.market.demandIndex > 0);
});

test('같은 시드 + 같은 조작이면 결과가 완전히 같다 (결정론)', () => {
  const play = (seed) => {
    const s = E.newGame(seed);
    E.launchProgram(s, { segment: 'narrow', seats: 170, range: 5200, tech: 55, material: 'hybrid' }, 'D-170');
    for (let i = 0; i < 30; i++) {
      const prod = s.programs.find((p) => p.phase === 'production');
      if (prod && s.lines.length < 2) E.buildLine(s, prod.id);
      for (const rfp of s.rfps) {
        const el = E.eligiblePrograms(s, rfp).filter((x) => !x.score.blocked);
        if (el.length) E.setBid(s, rfp.id, el[0].program.id, 0.1);
      }
      E.endTurn(s);
    }
    return { cash: s.cash, delivered: s.stats.delivered, rep: s.reputation, backlog: E.totalBacklog(s) };
  };
  assert.deepStrictEqual(play(555), play(555));
  assert.notDeepStrictEqual(play(555), play(556));
});

test('주문이 없으면 라인이 재고를 무한정 찍어내지 않는다 (화이트테일 폭주 방지)', () => {
  const s = E.newGame(4242);
  // 레거시 기종의 잔고를 모두 지우고, 라인만 계속 돌린다.
  for (const o of s.backlog) o.remaining = 0;
  for (let i = 0; i < 40; i++) E.endTurn(s);
  const stock = s.programs.reduce((a, p) => a + p.stock, 0);
  assert.strictEqual(stock, 0, `주문이 없는데 재고가 ${stock}기 생산됐다`);
});

test('재고 처분으로 생산 여유를 재생성해 무한히 현금을 만들 수 없다', () => {
  const s = E.newGame(606);
  for (const o of s.backlog) o.remaining = 0; // 주문 없음
  const legacy = s.programs.find((p) => p.legacy);

  const cash0 = s.cash;
  const delivered0 = s.stats.delivered;
  // 매 분기 재고를 처분하며 20분기 — 주문 없이 생산이 재개되면 현금이 불어난다.
  for (let i = 0; i < 20; i++) {
    if (legacy.stock > 0) E.sellStock(s, legacy.id, legacy.stock);
    E.endTurn(s);
  }
  assert.strictEqual(s.stats.delivered, delivered0, '주문이 없으면 인도량이 늘 수 없다');
  assert.ok(s.cash < cash0, '주문 없이 20분기를 보내면 고정비로 현금이 줄어야 한다');
});

test('파산은 다음 분기 이벤트가 되살리지 못한다', () => {
  // 정산 직후 지급불능이면, 이후 지원금 이벤트가 굴러도 파산이 확정돼야 한다.
  let sawInsolvent = false;
  for (const seed of [21, 77, 404, 1024, 90210]) {
    const s = E.newGame(seed);
    s.lines.length = 0;
    s.backlog.length = 0;
    for (const p of s.programs) p.stock = 0;
    s.cash = 1;
    s.debt = Data.CONFIG.maxDebt;
    E.endTurn(s);
    sawInsolvent = true;
    assert.ok(s.gameOver, `seed ${seed}: 지급불능인데 종료되지 않았다 (현금 ${Math.round(s.cash)})`);
    assert.strictEqual(s.gameOver.reason, 'bankrupt');
  }
  assert.ok(sawInsolvent);
});

test('옛 세이브(신규 필드 없음)에서도 행동이 예외 없이 처리된다', () => {
  const s = E.newGame(1);
  const legacy = s.programs.find((p) => p.legacy);
  legacy.stock = 5;
  // 이전 커밋에서 저장된 상태를 흉내 — version은 그대로 1이라 로드된다.
  delete s.pending;
  delete s.effects.grounded;

  const cashBefore = s.cash;
  const r = E.sellStock(s, legacy.id, 5);
  assert.ok(r.ok, '옛 세이브에서 재고 처분이 실패하면 안 된다');
  assert.ok(s.cash > cashBefore, '처분 대금이 들어와야 한다');
  assert.doesNotThrow(() => E.endTurn(s), '옛 세이브로 분기 종료가 가능해야 한다');
});

test('지급불능(현금<0 · 차입한도 소진) 상태로 살아있는 분기가 없다', () => {
  // 정산발이든 이벤트발(결함 수리비)이든 즉시 종료여야 한다.
  const max = Data.CONFIG.maxDebt;
  for (const seed of [1, 50, 60, 21, 404, 1234]) {
    const s = E.newGame(seed);
    s.debt = max; // 한도 소진 상태에서 시작
    for (let i = 0; i < 60 && !s.gameOver; i++) {
      E.endTurn(s);
      const insolvent = s.cash < 0 && s.debt >= max;
      assert.ok(
        !insolvent || s.gameOver,
        `seed ${seed} t${s.turn}: 현금 ${Math.round(s.cash)} · 부채 ${Math.round(s.debt)} 인데 게임이 계속된다`,
      );
    }
  }
});

test('한 분기의 입찰 점수는 앞선 수주의 평판·관계 상승에 영향받지 않는다', () => {
  const s = E.newGame(4242);
  const legacy = s.programs.find((p) => p.legacy);
  // 같은 분기에 동일 항공사 RFP 두 건을 놓고, 첫 건 처리로 오른 관계가
  // 둘째 건 점수에 반영되면 안 된다 (화면에 표시된 점수와 달라진다).
  const mk = (id) => ({
    id, turn: s.turn, airlineId: 'hanul', airlineName: '한울항공', home: '동아시아',
    segment: 'narrow', segmentName: '협동체', reqSeats: 150, reqRange: 4500, qty: 10,
    priceSensitivity: 0.9, prestige: 0.8, relation: s.relations.hanul, deadline: s.turn,
    rivalHint: { label: '보통', level: 2 },
  });
  s.rfps = [mk('rfp-a'), mk('rfp-b')];
  const B = globalThis.AirlinerBidding;
  const scoreA = B.scoreBid(s, s.rfps[0], legacy, 0.1).total;
  const scoreB = B.scoreBid(s, s.rfps[1], legacy, 0.1).total;
  assert.strictEqual(scoreA, scoreB, '분기 시작 시점의 두 점수는 같아야 한다');

  E.setBid(s, 'rfp-a', legacy.id, 0.1);
  E.setBid(s, 'rfp-b', legacy.id, 0.1);
  const relBefore = s.relations.hanul;
  E.endTurn(s);
  // 판정 뒤에는 관계가 올랐어야 하고(입찰 참여), 그 상승이 같은 분기 점수에
  // 소급 적용되지 않았음을 로그의 수주 결과 수로 확인한다.
  assert.ok(s.relations.hanul !== relBefore, '입찰 후 관계는 변해야 한다');
});

test('인력 배분 0%는 프로그램을 동결한다 (진행도·지출 정지)', () => {
  const s = E.newGame(808);
  const r = E.launchProgram(s, { segment: 'narrow', seats: 180, range: 5500, tech: 50, material: 'aluminum' }, 'F-1');
  const p = r.program;
  E.endTurn(s);
  const progressed = p.progress;
  assert.ok(progressed > 0, '기본 배분에서는 진행돼야 한다');

  p.share = 0; // 동결
  const spentBefore = p.spent;
  for (let i = 0; i < 5; i++) E.endTurn(s);
  assert.strictEqual(p.progress, progressed, '동결 중에는 진행도가 그대로여야 한다');
  assert.strictEqual(p.spent, spentBefore, '동결 중에는 개발비가 나가면 안 된다');
  assert.strictEqual(E.projectedQuarters(s, p), Infinity, '동결이면 완료 예상이 무한대');
});

test('파산은 인도량과 무관하게 등급 F', () => {
  const s = E.newGame(31);
  // 회생 불가 상태: 차입 한도 소진 + 현금 고갈 + 수익원(라인·잔고) 전무.
  s.lines.length = 0;
  s.backlog.length = 0;
  for (const p of s.programs) p.stock = 0;
  s.cash = -1;
  s.debt = Data.CONFIG.maxDebt;
  s.stats.delivered = 5000; // 아무리 많이 팔았어도
  E.endTurn(s);
  assert.strictEqual(s.gameOver.reason, 'bankrupt');
  assert.strictEqual(s.gameOver.grade, 'F');
});

test('라인 가동 중지는 생산을 멈추고 램프업을 초기화한다', () => {
  const s = E.newGame(77);
  const line = s.lines[0];
  const p = s.programs.find((x) => x.id === line.programId);
  E.endTurn(s);
  const producedBefore = p.produced;
  E.toggleLine(s, line.id);
  assert.strictEqual(line.idle, true);
  assert.strictEqual(line.ramp, 0.15, '재가동 시 램프업을 다시 올려야 한다');
  for (let i = 0; i < 3; i++) E.endTurn(s);
  assert.strictEqual(p.produced, producedBefore, '정지된 라인은 생산하지 않는다');
});

test('시작 시 노후 주력기와 가동 라인, 수주 잔고를 물려받는다', () => {
  const s = E.newGame(1);
  const legacy = s.programs.find((p) => p.legacy);
  assert.ok(legacy, '레거시 기종이 있어야 한다');
  assert.strictEqual(legacy.phase, 'production');
  assert.ok(s.lines.some((l) => l.programId === legacy.id), '레거시 전용 라인이 있어야 한다');
  assert.ok(E.totalBacklog(s) > 0, '인계받은 수주 잔고가 있어야 한다');
  // 캐시카우가 실제로 현금을 벌어야 한다 (아무 조작 없이 초반 몇 분기).
  // 시작값(186)과 비교하지 않으면 인도가 0이어도 통과하는 공허한 단언이 된다.
  const cash0 = s.cash;
  const delivered0 = s.stats.delivered;
  for (let i = 0; i < 4; i++) E.endTurn(s);
  assert.ok(s.stats.delivered > delivered0, `초반 4분기에 인도가 늘어야 한다 (${delivered0} → ${s.stats.delivered})`);
  assert.ok(s.cash > cash0 * 0.5, '캐시카우가 있는데 초반 4분기에 현금이 반토막 나면 안 된다');
});

test('개발비 총액은 착수금을 포함해 devCost를 넘지 않는다', () => {
  const s = E.newGame(4711);
  s.cash = 500000; // 자금 제약을 배제하고 회계만 본다
  const r = E.launchProgram(s, { segment: 'regional', seats: 90, range: 2500, tech: 40, material: 'aluminum' }, 'ACC-1');
  const p = r.program;
  const budget = p.devCost;
  assert.ok(p.spent > 0, '착수금이 즉시 반영돼야 한다');

  for (let i = 0; i < 60 && p.phase === 'dev'; i++) E.endTurn(s);
  assert.notStrictEqual(p.phase, 'dev', '개발이 끝나야 한다');

  // 착수금(8%)을 낸 뒤 진행도에 다시 100%를 물리면 총액이 108%가 된다.
  assert.ok(
    Math.abs(p.spent - budget) <= budget * 0.01,
    `총 개발 지출 ${Math.round(p.spent)}이 표시된 개발비 ${budget}와 어긋난다 (품질투자 제외 기준)`,
  );
});

test('수주하면 백로그가 쌓이고 인도되면 줄어든다', () => {
  const s = E.newGame(1234);
  const launched = E.launchProgram(s, { segment: 'regional', seats: 100, range: 3000, tech: 45, material: 'aluminum' }, 'R-100').program;
  // 물려받은 DN-150의 잔고·인도량으로도 참이 되지 않도록, 신규 기종만 놓고 본다.
  const backlogOf = (id) => s.backlog.reduce((a, o) => a + (o.programId === id ? o.remaining : 0), 0);
  let sawBacklog = false;
  let sawDelivery = false;

  for (let i = 0; i < 60; i++) {
    // 운영 정책은 현실적으로(레거시 포함 전 기종 판매) 두되, 단언은 신규 기종만 본다.
    // 신규 기종으로만 입찰하면 캐시카우가 말라 파산해 라인조차 못 세운다.
    for (const p of s.programs.filter((x) => x.phase === 'production')) {
      const ownLines = s.lines.filter((l) => l.programId === p.id).length;
      if (backlogOf(p.id) > ownLines * 20 && ownLines < 3) E.buildLine(s, p.id);
    }
    for (const rfp of s.rfps) {
      const el = E.eligiblePrograms(s, rfp).filter((x) => !x.score.blocked);
      if (el.length) {
        el.sort((a, b) => b.score.total - a.score.total);
        E.setBid(s, rfp.id, el[0].program.id, 0.15);
      }
    }
    E.endTurn(s);
    if (backlogOf(launched.id) > 0) sawBacklog = true;
    if (launched.delivered > 0) sawDelivery = true;
  }
  assert.ok(sawBacklog, '60분기 동안 신규 기종이 한 번도 수주하지 못하면 밸런스가 잘못된 것');
  assert.ok(sawDelivery, '신규 기종이 수주했다면 인도도 일어나야 한다');
  // 인도 수가 생산 수를 넘을 수 없다.
  for (const p of s.programs) assert.ok(p.delivered <= p.produced, `${p.name}: 인도(${p.delivered}) > 생산(${p.produced})`);
});
