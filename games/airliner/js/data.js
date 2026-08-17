/*
 * 정적 밸런스 데이터. 모든 금액 단위는 M$(백만 달러), 기간 단위는 분기.
 * 여기 숫자만 만져도 게임 난이도가 바뀌도록 로직(engine)과 분리해 둔다.
 */
(function (root) {
  'use strict';

  const CONFIG = {
    startYear: 1998,
    totalTurns: 80, // 20년
    startCash: 4200,
    startDebt: 1500,
    startReputation: 48,
    startEngineers: 3400,
    maxDebt: 14000,
    interestPerQuarter: 0.016,
    engineerCostPerQuarter: 0.035, // 1명당 M$
    engineerHireCost: 0.06, // 채용 일시금 (1명당)
    fixedOverheadPerQuarter: 55,
    lineOverheadPerLine: 9,
    defaultProgramShare: 50, // 신규 프로그램의 기본 인력 배분(%)
    launchUpfrontRate: 0.08, // 개발 착수금 (총 개발비 대비). 나머지는 진행도에 비례해 집행된다.
    depositRate: 0.15, // 수주 시 선수금
    inventoryHoldingCost: 0.004, // 재고 1기당 분기 유지비 (원가 대비)
    learningExponent: Math.log(0.87) / Math.log(2), // 87% 학습곡선
    // 바닥값이 높으면 누적 40기쯤에서 원가 개선이 멈춰버린다. 0.28이면 약 560기까지
    // 완만하게 내려가, 장기 양산이 실제로 보상받는다.
    learningFloor: 0.28,
    firstUnitPremium: 1.9, // T1 원가 = 표준원가 × 이 값
    maxDiscount: 0.35,
    rampPerQuarter: 0.22, // 라인 가동률 상승 속도
    // 주문 잔량을 넘어 미리 만들어 둘 수 있는 기수(라인별). 취소분과 함께
    // 화이트테일 재고의 원천이 되지만, 무한 생산은 막는다.
    speculativeBuffer: 3,
  };

  /** 기체 세그먼트 — 설계 가능 범위와 기준점 */
  const SEGMENTS = {
    regional: {
      id: 'regional',
      name: '리저널기',
      desc: '단거리 지선. 개발이 싸고 빠르지만 대당 이익이 얇다.',
      seats: { min: 60, max: 130, ref: 90 },
      range: { min: 1200, max: 4800, ref: 2500 },
      devBase: 2100,
      devQuarters: 11,
      engineersNeeded: 2000,
      unitCostBase: 31,
      listPriceBase: 42,
      lineCost: 430,
      lineMaxRate: 13, // 분기당 최대 생산 기수
      certQuarters: 3,
    },
    narrow: {
      id: 'narrow',
      name: '협동체',
      desc: '시장의 심장. 물량이 압도적이라 라인만 돌면 현금이 돈다.',
      seats: { min: 120, max: 240, ref: 180 },
      range: { min: 2800, max: 7800, ref: 5500 },
      devBase: 6400,
      devQuarters: 15,
      engineersNeeded: 3800,
      unitCostBase: 60,
      listPriceBase: 94,
      lineCost: 880,
      lineMaxRate: 16,
      certQuarters: 4,
    },
    wide: {
      id: 'wide',
      name: '광동체',
      desc: '장거리 간판. 개발비가 회사를 삼킬 수 있지만 평판이 크게 오른다.',
      seats: { min: 230, max: 480, ref: 320 },
      range: { min: 6500, max: 16500, ref: 12000 },
      devBase: 11000,
      devQuarters: 19,
      engineersNeeded: 6000,
      unitCostBase: 162,
      listPriceBase: 258,
      lineCost: 1580,
      lineMaxRate: 6,
      certQuarters: 5,
    },
  };

  const SEGMENT_ORDER = ['regional', 'narrow', 'wide'];

  /** 주 구조재 선택 — 연비/개발비/개발리스크 트레이드오프 */
  const MATERIALS = {
    aluminum: {
      id: 'aluminum',
      name: '알루미늄 합금',
      desc: '검증된 공법. 개발이 순탄하고 원가가 낮다.',
      devCostMult: 1.0,
      devTimeMult: 1.0,
      unitCostMult: 1.0,
      efficiencyBonus: 0,
      comfortBonus: 0,
      riskBonus: 0,
    },
    hybrid: {
      id: 'hybrid',
      name: '복합재 부분 적용',
      desc: '날개·꼬리만 복합재. 무난한 절충안.',
      devCostMult: 1.18,
      devTimeMult: 1.08,
      unitCostMult: 1.06,
      efficiencyBonus: 7,
      comfortBonus: 4,
      riskBonus: 0.05,
    },
    composite: {
      id: 'composite',
      name: '전기체 복합재',
      desc: '동체까지 복합재. 연비와 객실이 압도적이지만 개발이 미끄러진다.',
      devCostMult: 1.35,
      devTimeMult: 1.25,
      unitCostMult: 1.15,
      efficiencyBonus: 18,
      comfortBonus: 11,
      riskBonus: 0.14,
    },
  };

  /** 항공사 — 수주전 상대이자 관계 관리 대상 */
  const AIRLINES = [
    { id: 'hanul', name: '한울항공', home: '동아시아', bias: 'narrow', priceSensitivity: 0.9, prestige: 0.8 },
    { id: 'carta', name: '카르타 에어', home: '중동', bias: 'wide', priceSensitivity: 0.5, prestige: 1.25 },
    { id: 'nordic', name: '노르딕윙스', home: '북유럽', bias: 'regional', priceSensitivity: 1.15, prestige: 0.7 },
    { id: 'panamer', name: '판아메르 항공', home: '북미', bias: 'narrow', priceSensitivity: 1.05, prestige: 1.0 },
    { id: 'asialink', name: '아시아링크', home: '동남아', bias: 'wide', priceSensitivity: 1.2, prestige: 0.85 },
    { id: 'albion', name: '알비온 항공', home: '서유럽', bias: 'wide', priceSensitivity: 0.75, prestige: 1.15 },
    { id: 'meridian', name: '메리디안 항공', home: '남미', bias: 'narrow', priceSensitivity: 1.3, prestige: 0.6 },
    { id: 'sahara', name: '사하라 에어', home: '아프리카', bias: 'regional', priceSensitivity: 1.25, prestige: 0.55 },
    { id: 'oceanic', name: '오세아닉', home: '오세아니아', bias: 'wide', priceSensitivity: 0.95, prestige: 0.9 },
    { id: 'kosmo', name: '코스모항공', home: '중앙아시아', bias: 'narrow', priceSensitivity: 1.35, prestige: 0.5 },
    { id: 'lumen', name: '루멘 에어라인', home: '북미', bias: 'regional', priceSensitivity: 1.1, prestige: 0.75 },
    { id: 'vertex', name: '버텍스 제트', home: '서유럽', bias: 'narrow', priceSensitivity: 1.4, prestige: 0.45 },
  ];

  /**
   * 경쟁 제조사 — 세그먼트별 기본 경쟁력. 이 값은 플레이어의 입찰 점수(0~100)와
   * 같은 척도에서 직접 비교되므로, 세그먼트별 최고치가 곧 "이겨야 할 문턱"이다.
   * 평범한 초도 설계가 50대 중반을 내므로 문턱은 그 근처에 둬 첫 수주가 가능하게 한다.
   */
  const COMPETITORS = [
    { id: 'aurelia', name: '아우렐리아 에어로스페이스', strength: { regional: 42, narrow: 55, wide: 60 } },
    { id: 'vostok', name: '보스토크 항공기공업', strength: { regional: 47, narrow: 48, wide: 44 } },
    { id: 'novaaero', name: '노바에어로', strength: { regional: 56, narrow: 45, wide: 36 } },
  ];

  /** 경쟁사 경쟁력 상한 — 이벤트 누적으로 무한정 강해지는 것을 막는다. */
  const RIVAL_STRENGTH_CAP = 78;

  /**
   * 랜덤 이벤트. apply(state, ctx)는 engine이 주입한 헬퍼로 상태를 바꾸고
   * 사용자에게 보여줄 문장을 반환한다. weight는 상대 출현 빈도.
   */
  const EVENTS = [
    {
      id: 'fuel_spike',
      name: '유가 급등',
      weight: 10,
      apply: (s, h) => {
        const d = h.rng.range(0.18, 0.42);
        s.market.fuelIndex = Math.min(2.2, s.market.fuelIndex + d);
        return `국제 유가가 치솟았다. 항공사들이 연비를 최우선으로 보기 시작한다. (연료지수 +${d.toFixed(2)})`;
      },
    },
    {
      id: 'fuel_drop',
      name: '유가 안정',
      weight: 8,
      apply: (s, h) => {
        const d = h.rng.range(0.12, 0.3);
        s.market.fuelIndex = Math.max(0.5, s.market.fuelIndex - d);
        return `유가가 내려앉았다. 연비 프리미엄이 힘을 잃고 가격 경쟁이 거세진다. (연료지수 -${d.toFixed(2)})`;
      },
    },
    {
      id: 'boom',
      name: '항공 수요 호황',
      weight: 9,
      apply: (s, h) => {
        const d = h.rng.range(0.15, 0.35);
        s.market.demandIndex = Math.min(2.0, s.market.demandIndex + d);
        return `여객 수요가 폭발했다. 항공사들이 앞다퉈 발주 계획을 앞당긴다. (수요지수 +${d.toFixed(2)})`;
      },
    },
    {
      id: 'recession',
      name: '경기 침체',
      weight: 9,
      apply: (s, h) => {
        const d = h.rng.range(0.2, 0.45);
        s.market.demandIndex = Math.max(0.35, s.market.demandIndex - d);
        return `경기가 꺾였다. 신규 발주가 얼어붙고 기존 계약도 흔들린다. (수요지수 -${d.toFixed(2)})`;
      },
    },
    {
      id: 'order_cancel',
      name: '발주 취소',
      weight: 7,
      condition: (s) => s.backlog.some((o) => o.remaining > 0),
      apply: (s, h) => {
        const live = s.backlog.filter((o) => o.remaining > 0);
        const order = h.rng.pick(live);
        const cut = Math.max(1, Math.round(order.remaining * h.rng.range(0.2, 0.5)));
        order.remaining -= cut;
        order.cancelled = (order.cancelled || 0) + cut;
        h.reputation(-2);
        return `${order.airlineName}이(가) ${order.programName} ${cut}기 발주를 취소했다. 선수금은 위약금으로 남는다.`;
      },
    },
    {
      id: 'strike',
      name: '생산직 파업',
      weight: 6,
      condition: (s) => s.lines.length > 0,
      apply: (s, h) => {
        s.effects.strikeQuarters = h.rng.int(1, 2);
        return `조립 노조가 파업에 돌입했다. ${s.effects.strikeQuarters}개 분기 동안 생산이 반토막 난다.`;
      },
    },
    {
      id: 'supplier_delay',
      name: '공급망 차질',
      weight: 8,
      condition: (s) => s.lines.length > 0,
      apply: (s, h) => {
        s.effects.supplyQuarters = h.rng.int(1, 3);
        return `1차 협력사가 납기를 놓쳤다. ${s.effects.supplyQuarters}개 분기 동안 생산율이 25% 깎인다.`;
      },
    },
    {
      id: 'cert_snag',
      name: '인증 지연',
      weight: 7,
      condition: (s) => s.programs.some((p) => p.phase === 'cert'),
      apply: (s, h) => {
        const p = h.rng.pick(s.programs.filter((x) => x.phase === 'cert'));
        const q = h.rng.int(1, 3);
        p.certRemaining += q;
        return `감항당국이 ${p.name}의 비행제어 소프트웨어에 추가 시험을 요구했다. 인증이 ${q}분기 밀린다.`;
      },
    },
    {
      id: 'tech_grant',
      name: '정부 연구지원금',
      weight: 6,
      apply: (s, h) => {
        const amount = Math.round(h.rng.range(180, 520));
        s.cash += amount;
        return `국책 항공기술 과제에 선정돼 지원금 ${h.fmt(amount)}을(를) 수령했다.`;
      },
    },
    {
      id: 'defect',
      name: '운항 중 결함',
      weight: 8,
      condition: (s) => s.programs.some((p) => p.delivered > 0),
      apply: (s, h) => {
        // 결함 위험이 높은 기종일수록 자주 걸린다. 균등 추첨이면 품질 투자로 낮춘
        // defectRisk 가 중대/경미 판정에만 쓰여 "위험 25% 감소"가 빈도에 반영되지 않는다.
        const fleet = s.programs.filter((p) => p.delivered > 0);
        const p = h.pickWeighted(fleet, (x) => x.defectRisk);
        const severity = h.rng.next() < p.defectRisk ? 'major' : 'minor';
        if (severity === 'major') {
          const cost = Math.round(p.delivered * p.unitCostBase * h.rng.range(0.08, 0.2));
          s.cash -= cost;
          h.reputation(-9);
          s.effects.groundedProgram = p.id;
          s.effects.groundedQuarters = h.rng.int(1, 3);
          return `${p.name}에서 중대 결함이 발견돼 전 기체가 운항 정지됐다. 수리·보상에 ${h.fmt(cost)} 소요, 인도도 멈춘다.`;
        }
        const cost = Math.round(p.delivered * h.rng.range(0.15, 0.5));
        s.cash -= cost;
        h.reputation(-2);
        return `${p.name}에 경미한 결함 서비스회보가 발행됐다. 대응 비용 ${h.fmt(cost)}.`;
      },
    },
    {
      id: 'rival_launch',
      name: '경쟁사 신형 투입',
      weight: 9,
      apply: (s, h) => {
        const c = h.rng.pick(s.competitors);
        const seg = h.rng.pick(['regional', 'narrow', 'wide']);
        const gain = h.rng.range(3, 8);
        c.strength[seg] = Math.min(RIVAL_STRENGTH_CAP, c.strength[seg] + gain);
        return `${c.name}이(가) ${SEGMENTS[seg].name} 신형을 발표했다. 해당 시장 경쟁이 거세진다.`;
      },
    },
    {
      id: 'rival_trouble',
      name: '경쟁사 악재',
      weight: 6,
      apply: (s, h) => {
        const c = h.rng.pick(s.competitors);
        const seg = h.rng.pick(['regional', 'narrow', 'wide']);
        const loss = h.rng.range(4, 9);
        c.strength[seg] = Math.max(25, c.strength[seg] - loss);
        h.reputation(2);
        return `${c.name}의 ${SEGMENTS[seg].name} 프로그램이 대규모 지연에 빠졌다. 반사이익이 우리에게 온다.`;
      },
    },
    {
      id: 'engineer_poach',
      name: '핵심 인력 유출',
      weight: 6,
      condition: (s) => s.engineers > 1500,
      apply: (s, h) => {
        const loss = Math.round(s.engineers * h.rng.range(0.05, 0.12));
        s.engineers -= loss;
        return `경쟁사가 설계팀을 통째로 스카우트했다. 엔지니어 ${loss.toLocaleString('ko-KR')}명 이탈.`;
      },
    },
    {
      id: 'award',
      name: '업계 수상',
      weight: 5,
      condition: (s) => s.programs.some((p) => p.phase === 'production'),
      apply: (s, h) => {
        h.reputation(h.rng.int(4, 8));
        return '항공업계 연례 시상식에서 올해의 기체상을 받았다. 브랜드 신뢰도가 올라간다.';
      },
    },
    {
      id: 'credit_squeeze',
      name: '신용 경색',
      weight: 5,
      apply: (s, h) => {
        s.effects.rateBump = h.rng.range(0.004, 0.011);
        s.effects.rateBumpQuarters = h.rng.int(2, 5);
        return `금융시장이 경색됐다. 당분간 차입 이자율이 분기 ${(s.effects.rateBump * 100).toFixed(1)}%p 오른다.`;
      },
    },
  ];

  root.AirlinerData = {
    CONFIG,
    SEGMENTS,
    SEGMENT_ORDER,
    MATERIALS,
    AIRLINES,
    COMPETITORS,
    RIVAL_STRENGTH_CAP,
    EVENTS,
  };
})(typeof globalThis !== 'undefined' ? globalThis : this);
