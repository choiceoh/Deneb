/*
 * 수주전 — RFP 생성과 입찰 점수 계산.
 *
 * 점수는 0~100 스케일이며 경쟁사 점수와 직접 비교한다. 항목 가중치가 시장 상황
 * (연료지수·항공사 성향)에 따라 움직이는 게 이 게임의 핵심 긴장이다.
 */
(function (root) {
  'use strict';

  const { SEGMENTS, AIRLINES, CONFIG, RIVAL_STRENGTH_CAP, RIVAL_STRENGTH_FLOOR } = root.AirlinerData;
  const { clamp } = root.AirlinerDesign;
  const Fleet = root.AirlinerFleet;

  /**
   * 경쟁사 응찰 우위. 카탈로그 점수는 "기종의 실력"일 뿐이고, 실제 수주전에서
   * 기존 기종은 개발비를 이미 회수했기 때문에 가격 공세 여지가 크다 — 그만큼 문턱을 올린다.
   *
   * 밸런스상 중요: 예전에는 경쟁사마다 난수를 뽑아 최댓값을 쓰는 방식이라 경쟁사 수(3)에서
   * 오는 +5점 정도의 편향이 문턱에 섞여 있었다. 지금은 제조사가 8곳이라 그 방식을 쓰면
   * 문턱이 제조사 수에 따라 멋대로 오르므로, 추첨은 한 번만 하고 편향을 이 상수로 명시한다.
   */
  const RIVAL_BID_EDGE = 4;

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
      rivalHint: rivalBand(state, segmentId, seats, range),
    };
  }

  /**
   * 플레이어에게 보여줄 대략적 경쟁 강도 — 정확한 숫자는 감추되,
   * 어느 회사의 어느 기종과 붙는지는 알려준다(현실에서도 그건 안다).
   */
  function rivalBand(state, segmentId, reqSeats, reqRange) {
    const offer = bestOffering(state, segmentId, reqSeats, reqRange);
    const best = offer ? offer.score : RIVAL_STRENGTH_FLOOR;
    const rival = offer ? offer.name : '—';
    if (best >= 70) return { label: '매우 치열', level: 4, rival };
    if (best >= 60) return { label: '치열', level: 3, rival };
    if (best >= 50) return { label: '보통', level: 2, rival };
    return { label: '느슨', level: 1, rival };
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

  /**
   * 이 시점·이 요구사양에서 경쟁사들이 실제로 내놓을 수 있는 최고의 제안.
   * 난수를 쓰지 않는다 — 경쟁사 수가 늘어도 문턱이 따라 오르지 않도록,
   * 불확실성은 rivalScore 에서 딱 한 번만 얹는다.
   *
   * @param {number|null} reqSeats null 이면 적합도를 빼고 세그먼트 대표 기종만 고른다
   */
  function bestOffering(state, segmentId, reqSeats, reqRange) {
    const year = Fleet.yearAt(state.turn, CONFIG.startYear);
    const pool = Fleet.availableTypes(segmentId, year);

    let best = null;
    for (const c of state.competitors) {
      const drift = (c.drift && c.drift[segmentId]) || 0;
      for (const type of pool) {
        if (type.maker !== c.id) continue;
        const score = Fleet.typeScore(type, state.market.fuelIndex, reqSeats, reqRange) + drift;
        if (!best || score > best.score) best = { maker: c, type, score };
      }
    }
    if (!best) return null;

    return {
      maker: best.maker,
      type: best.type,
      name: `${best.maker.name} ${best.type.name}`,
      score: clamp(best.score, RIVAL_STRENGTH_FLOOR, RIVAL_STRENGTH_CAP),
    };
  }

  function rivalScore(state, rfp, rng) {
    const offer = bestOffering(state, rfp.segment, rfp.reqSeats, rfp.reqRange);
    // 그 시점 그 세그먼트에 아무도 없으면(카탈로그 공백) 시장 평균이 문턱이 된다.
    if (!offer) return { name: '—', score: RIVAL_STRENGTH_FLOOR };

    const score = clamp(offer.score + rng.normal(RIVAL_BID_EDGE, 6), RIVAL_STRENGTH_FLOOR, RIVAL_STRENGTH_CAP);
    return { name: offer.name, score: Math.round(score * 10) / 10 };
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
    } else if (margin >= -4) {
      outcome = 'split';
      qty = Math.max(1, Math.round(rfp.qty * 0.5));
    } else {
      outcome = 'lose';
    }

    return { outcome, qty, rivalName: rival.name, rivalScore: rival.score, margin: Math.round(margin * 10) / 10 };
  }

  root.AirlinerBidding = { generateRfps, scoreBid, resolveBid, rivalScore, rivalBand, bestOffering, CONFIG };
})(typeof globalThis !== 'undefined' ? globalThis : this);
