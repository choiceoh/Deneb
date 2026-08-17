/*
 * 수주전 — RFP 생성과 입찰 점수 계산.
 *
 * 점수는 0~100 스케일이며 경쟁사 점수와 직접 비교한다. 항목 가중치가 시장 상황
 * (연료지수·항공사 성향)에 따라 움직이는 게 이 게임의 핵심 긴장이다.
 */
(function (root) {
  'use strict';

  const { SEGMENTS, AIRLINES, CONFIG } = root.AirlinerData;
  const { clamp } = root.AirlinerDesign;

  /** 해당 분기에 새로 뜨는 RFP 목록을 만든다. */
  function generateRfps(state, rng) {
    const rfps = [];
    const demand = state.market.demandIndex;
    // 수요지수가 낮으면 입찰 자체가 줄어든다.
    let count = 0;
    if (rng.next() < clamp(demand * 0.85, 0.1, 0.95)) count++;
    if (rng.next() < clamp(demand * 0.55, 0.05, 0.85)) count++;
    if (rng.next() < clamp(demand * 0.22, 0.02, 0.5)) count++;

    for (let i = 0; i < count; i++) {
      rfps.push(makeRfp(state, rng));
    }
    return rfps;
  }

  function makeRfp(state, rng) {
    const airline = rng.pick(AIRLINES);
    // 항공사는 자기 성향 세그먼트를 자주, 그러나 항상은 아니게 고른다.
    const segmentId = rng.next() < 0.55 ? airline.bias : rng.pick(['regional', 'narrow', 'wide']);
    const seg = SEGMENTS[segmentId];

    const seats = Math.round(rng.range(seg.seats.min * 1.05, seg.seats.max * 0.92));
    const range = Math.round(rng.range(seg.range.min * 1.1, seg.range.max * 0.88));

    // 발주 규모: 세그먼트가 작을수록 대량. 수요지수가 곱해진다.
    const baseQty = segmentId === 'regional' ? rng.int(8, 45) : segmentId === 'narrow' ? rng.int(10, 70) : rng.int(4, 26);
    const qty = Math.max(3, Math.round(baseQty * clamp(state.market.demandIndex, 0.4, 1.8)));

    const relation = state.relations[airline.id] ?? 40;

    return {
      id: 'rfp-' + state.nextId++,
      turn: state.turn,
      airlineId: airline.id,
      airlineName: airline.name,
      home: airline.home,
      segment: segmentId,
      segmentName: seg.name,
      reqSeats: seats,
      reqRange: range,
      qty,
      priceSensitivity: airline.priceSensitivity,
      prestige: airline.prestige,
      relation,
      deadline: state.turn, // 이번 분기 안에 결정
      // 경쟁사 최고 점수는 입찰 확정 시점에 계산한다(플레이어가 미리 못 봄).
      rivalHint: rivalBand(state, segmentId),
    };
  }

  /** 플레이어에게 보여줄 대략적 경쟁 강도 — 정확한 숫자는 감춘다. */
  function rivalBand(state, segmentId) {
    const best = Math.max(...state.competitors.map((c) => c.strength[segmentId]));
    if (best >= 70) return { label: '매우 치열', level: 4 };
    if (best >= 60) return { label: '치열', level: 3 };
    if (best >= 50) return { label: '보통', level: 2 };
    return { label: '느슨', level: 1 };
  }

  /**
   * 우리 기체 한 종의 입찰 점수를 계산한다.
   * @returns {{total:number, parts:object, blocked:string|null, price:number}}
   */
  function scoreBid(state, rfp, program, discount) {
    const seg = SEGMENTS[rfp.segment];

    // 세그먼트가 다르면 애초에 후보가 아니다.
    if (program.segment !== rfp.segment) {
      return { total: 0, parts: {}, blocked: '세그먼트 불일치', price: 0 };
    }
    // 요구 항속의 90% 미만이면 노선 자체를 못 뛴다 — 실격.
    if (program.range < rfp.reqRange * 0.9) {
      return { total: 0, parts: {}, blocked: '항속거리 부족', price: 0 };
    }
    // 좌석이 요구의 80% 미만이면 수송력 미달 — 실격.
    if (program.seats < rfp.reqSeats * 0.8) {
      return { total: 0, parts: {}, blocked: '좌석수 부족', price: 0 };
    }

    // 좌석 적합도: 모자라도, 지나치게 커도 감점(공석은 곧 비용).
    const seatDelta = (program.seats - rfp.reqSeats) / rfp.reqSeats;
    const seatFit = seatDelta >= 0 ? clamp(1 - seatDelta * 1.35, 0, 1) : clamp(1 + seatDelta * 2.2, 0, 1);

    // 항속 적합도: 여유는 조금 좋지만 과하면 중량 낭비.
    const rangeDelta = (program.range - rfp.reqRange) / rfp.reqRange;
    const rangeFit = rangeDelta >= 0 ? clamp(1 - Math.max(0, rangeDelta - 0.1) * 0.9, 0, 1) : clamp(1 + rangeDelta * 4, 0, 1);

    const specFit = seatFit * 0.62 + rangeFit * 0.38;

    // 가격: 동급 기준가 대비 실효가격.
    const refPrice = seg.listPriceBase * Math.pow(rfp.reqSeats / seg.seats.ref, 0.95);
    const effPrice = program.listPrice * (1 - discount);
    const priceScore = clamp(1.6 - effPrice / refPrice, 0, 1);

    // 연비: 연료지수가 높을수록 이 항목의 가중치가 커진다.
    const effScore = clamp(program.efficiency / 100, 0, 1);
    const comfortScore = clamp(program.comfort / 100, 0, 1);
    const repScore = clamp(state.reputation / 100, 0, 1);
    const relScore = clamp((state.relations[rfp.airlineId] ?? 40) / 100, 0, 1);

    // 가중치를 시장 상황에 맞춰 재배분한 뒤 합이 1이 되도록 정규화한다.
    const w = {
      spec: 0.3,
      price: 0.27 * rfp.priceSensitivity,
      eff: 0.17 * clamp(state.market.fuelIndex, 0.4, 2.2),
      comfort: 0.07 * rfp.prestige,
      rep: 0.12,
      rel: 0.09,
    };
    const wsum = Object.values(w).reduce((a, b) => a + b, 0);

    const total =
      (100 *
        (specFit * w.spec +
          priceScore * w.price +
          effScore * w.eff +
          comfortScore * w.comfort +
          repScore * w.rep +
          relScore * w.rel)) /
      wsum;

    return {
      total: Math.round(total * 10) / 10,
      parts: {
        spec: Math.round(specFit * 100),
        price: Math.round(priceScore * 100),
        eff: Math.round(effScore * 100),
        comfort: Math.round(comfortScore * 100),
        rep: Math.round(repScore * 100),
        rel: Math.round(relScore * 100),
      },
      blocked: null,
      price: Math.round(effPrice * 10) / 10,
    };
  }

  /** 경쟁사 최종 점수 — 기본 경쟁력에 무작위 변동을 준다. */
  function rivalScore(state, rfp, rng) {
    let best = { name: '—', score: 0 };
    for (const c of state.competitors) {
      // 시간이 갈수록 업계 전체가 좋아진다(경쟁 인플레이션).
      const era = state.turn * 0.06;
      const s = c.strength[rfp.segment] + era + rng.normal(0, 6);
      if (s > best.score) best = { name: c.name, score: s };
    }
    best.score = Math.round(best.score * 10) / 10;
    return best;
  }

  /**
   * 입찰 결과 판정.
   * 근소한 차이(±4점)면 물량을 나눠 갖는다 — 실제 수주전처럼 완패/완승이 드물게.
   */
  function resolveBid(state, rfp, bid, rng) {
    const rival = rivalScore(state, rfp, rng);
    const margin = bid.score.total - rival.score;

    let outcome;
    let qty = 0;
    if (margin > 4) {
      outcome = 'win';
      qty = rfp.qty;
    } else if (margin > -4) {
      outcome = 'split';
      qty = Math.max(1, Math.round(rfp.qty * 0.5));
    } else {
      outcome = 'lose';
    }

    return { outcome, qty, rivalName: rival.name, rivalScore: rival.score, margin: Math.round(margin * 10) / 10 };
  }

  root.AirlinerBidding = { generateRfps, scoreBid, resolveBid, rivalScore, rivalBand, CONFIG };
})(typeof globalThis !== 'undefined' ? globalThis : this);
