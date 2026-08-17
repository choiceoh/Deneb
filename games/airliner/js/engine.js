/*
 * 게임 엔진 — 상태 생성, 플레이어 행동, 분기 정산.
 *
 * 규칙: 이 파일은 DOM을 모른다. 모든 함수는 state를 받아 state를 바꾸고 로그를 남긴다.
 * 덕분에 test/engine.test.cjs 에서 브라우저 없이 전체 시뮬레이션을 돌릴 수 있다.
 */
(function (root) {
  'use strict';

  const { CONFIG, SEGMENTS, AIRLINES, COMPETITORS, EVENTS } = root.AirlinerData;
  const { evaluate, unitCostAt, clamp } = root.AirlinerDesign;
  const { generateRfps, scoreBid, resolveBid } = root.AirlinerBidding;
  const { createRng } = root.AirlinerRng;

  /** 금액 표기: 1000M$ 이상은 B$로 접는다. */
  function fmtMoney(m) {
    const v = Math.round(m);
    if (Math.abs(v) >= 1000) return `$${(v / 1000).toFixed(2)}B`;
    return `$${v}M`;
  }

  function turnLabel(turn) {
    const year = CONFIG.startYear + Math.floor(turn / 4);
    const q = (turn % 4) + 1;
    return `${year}년 ${q}분기`;
  }

  // ─────────────────────────────── 상태 생성 ───────────────────────────────

  function newGame(seed, companyName) {
    const s = {
      version: 1,
      seed: seed >>> 0,
      rngState: seed >>> 0,
      company: companyName || '데네브 항공우주',
      turn: 0,
      nextId: 1,
      cash: CONFIG.startCash,
      debt: CONFIG.startDebt,
      reputation: CONFIG.startReputation,
      engineers: CONFIG.startEngineers,
      market: { fuelIndex: 1.0, demandIndex: 1.0 },
      effects: {
        strikeQuarters: 0,
        supplyQuarters: 0,
        grounded: {}, // programId → 남은 정지 분기수 (기종별로 독립)
        rateBump: 0,
        rateBumpQuarters: 0,
      },
      programs: [],
      lines: [],
      backlog: [],
      relations: {},
      competitors: COMPETITORS.map((c) => ({ id: c.id, name: c.name, strength: { ...c.strength } })),
      rfps: [],
      bids: {},
      log: [],
      history: [],
      stats: { delivered: 0, revenue: 0, rivalDelivered: 240, ordersWon: 0, bidsMade: 0 },
      // 분기 중 즉시 발생한 실적(재고 처분 등) — 다음 endTurn 리포트가 흡수한다.
      pending: { revenue: 0, delivered: 0 },
      events: [],
      gameOver: null,
    };
    for (const a of AIRLINES) s.relations[a.id] = 34 + (a.prestige < 0.8 ? 10 : 0);

    seedLegacyProgram(s);

    const rng = rngFor(s);
    s.rfps = generateRfps(s, rng);
    saveRng(s, rng);

    pushLog(
      s,
      'info',
      `${s.company} 경영을 인계받았다. 자본금 ${fmtMoney(s.cash)}, 차입금 ${fmtMoney(s.debt)}. 주력기 DN-150이 아직 현금을 벌어다 주지만, 설계는 이미 낡았다.`,
    );
    pushLog(s, 'info', '20년 안에 후속기를 띄워 시장을 잡아라. DN-150의 수명은 길지 않다.');
    return s;
  }

  /**
   * 시작 시점의 "노후 주력기". 이 게임의 출발점은 창업이 아니라 승계다.
   * 개발에는 5년 넘게 걸리는데 신생사는 그 사이 현금이 말라 죽으므로,
   * 플레이어에게 후속기 개발을 버텨낼 캐시카우를 쥐여준다.
   * 대신 연비가 낮아 유가가 오르거나 경쟁사가 신형을 내면 급속히 경쟁력을 잃는다.
   */
  function seedLegacyProgram(s) {
    const spec = { segment: 'narrow', seats: 150, range: 4800, tech: 38, material: 'aluminum' };
    const ev = evaluate(spec);
    const p = {
      id: 'prog-' + s.nextId++,
      name: 'DN-150',
      ...ev,
      phase: 'production',
      progress: 100,
      spent: ev.devCost,
      certRemaining: 0,
      qualityInvests: 1,
      share: 0,
      produced: 186, // 이미 학습곡선을 상당히 내려온 상태
      delivered: 186,
      stock: 0,
      launchTurn: -40,
      certTurn: -8,
      derivedFrom: null,
      legacy: true,
    };
    // 오랜 운용으로 초기 결함은 대부분 잡혔다.
    p.defectRisk = Math.round(p.defectRisk * 0.7 * 1000) / 1000;
    s.programs.push(p);

    s.lines.push({
      id: 'line-' + s.nextId++,
      programId: p.id,
      capacity: SEGMENTS.narrow.lineMaxRate,
      ramp: 1,
      partial: 0,
      idle: false,
      builtTurn: -8,
    });

    s.stats.delivered = p.delivered;

    // 인계받은 수주 잔고 — 초반 몇 년치 현금흐름.
    for (const o of [
      { id: 'panamer', name: '판아메르 항공', qty: 24 },
      { id: 'hanul', name: '한울항공', qty: 16 },
    ]) {
      s.backlog.push({
        id: 'ord-' + s.nextId++,
        airlineId: o.id,
        airlineName: o.name,
        programId: p.id,
        programName: p.name,
        qty: o.qty,
        remaining: o.qty,
        unitPrice: Math.round(p.listPrice * 0.92 * 10) / 10,
        wonTurn: -4,
      });
      s.relations[o.id] = 58;
    }
  }

  function rngFor(s) {
    const rng = createRng(s.rngState);
    return rng;
  }
  function saveRng(s, rng) {
    s.rngState = rng.getState();
  }

  function pushLog(s, kind, text) {
    s.log.unshift({ turn: s.turn, label: turnLabel(s.turn), kind, text });
    if (s.log.length > 250) s.log.length = 250;
  }

  // ─────────────────────────────── 플레이어 행동 ───────────────────────────────

  /** 신규 프로그램 착수. 착수금(개발비의 8%)을 즉시 지출한다. */
  function launchProgram(s, spec, name) {
    const evalSpec = evaluate(spec);
    const upfront = Math.round(evalSpec.devCost * CONFIG.launchUpfrontRate);
    if (s.cash < upfront) {
      return { ok: false, error: `착수금 ${fmtMoney(upfront)}이 부족합니다.` };
    }
    if (s.programs.filter((p) => p.phase === 'dev' || p.phase === 'cert').length >= 3) {
      return { ok: false, error: '동시에 개발 가능한 프로그램은 3개까지입니다.' };
    }

    const program = {
      id: 'prog-' + s.nextId++,
      name: name || `${SEGMENTS[evalSpec.segment].name}-${s.programs.length + 1}`,
      ...evalSpec,
      phase: 'dev',
      progress: 0,
      spent: upfront,
      certRemaining: evalSpec.certQuarters,
      qualityInvests: 0,
      share: CONFIG.defaultProgramShare,
      produced: 0,
      delivered: 0,
      stock: 0,
      launchTurn: s.turn,
      certTurn: null,
      derivedFrom: spec.derivedFrom || null,
    };
    s.cash -= upfront;
    s.programs.push(program);
    pushLog(
      s,
      'program',
      `${program.name} 개발 착수. 총 개발비 ${fmtMoney(program.devCost)}, 예상 ${program.devQuarters}분기, 필요 인력 ${program.engineersNeeded.toLocaleString('ko-KR')}명.`,
    );
    return { ok: true, program };
  }

  /** 파생형 착수용 설계 시드 — 원형의 기술/소재를 물려받는다. */
  function derivativeSpec(base, seatDelta) {
    const seg = SEGMENTS[base.segment];
    return {
      segment: base.segment,
      seats: clamp(base.seats + seatDelta, seg.seats.min, seg.seats.max),
      range: base.range,
      tech: base.tech,
      material: base.material,
      derivedFrom: { id: base.id, name: base.name },
    };
  }

  /** 품질 강화 투자 — 결함 위험을 25% 상대 감소. 프로그램당 3회까지. */
  function investQuality(s, programId) {
    const p = s.programs.find((x) => x.id === programId);
    if (!p) return { ok: false, error: '프로그램을 찾을 수 없습니다.' };
    if (p.qualityInvests >= 3) return { ok: false, error: '품질 투자는 3회까지입니다.' };
    const cost = Math.round(p.devCost * 0.06);
    if (s.cash < cost) return { ok: false, error: `${fmtMoney(cost)}이 부족합니다.` };
    s.cash -= cost;
    p.qualityInvests++;
    p.defectRisk = Math.round(p.defectRisk * 0.75 * 1000) / 1000;
    pushLog(s, 'program', `${p.name} 추가 시험·검증에 ${fmtMoney(cost)} 투입. 결함 위험 ${(p.defectRisk * 100).toFixed(1)}%로 하락.`);
    return { ok: true };
  }

  /** 개발 취소 — 투입 비용은 돌아오지 않는다. */
  function cancelProgram(s, programId) {
    const p = s.programs.find((x) => x.id === programId);
    if (!p || p.phase === 'production' || p.phase === 'cancelled') {
      return { ok: false, error: '취소할 수 없는 프로그램입니다.' };
    }
    p.phase = 'cancelled';
    adjustReputation(s, -4);
    pushLog(s, 'bad', `${p.name} 개발 중단. 매몰비용 ${fmtMoney(p.spent)}, 업계 신뢰가 흔들린다.`);
    return { ok: true };
  }

  /** 조립 라인 신설 — 인증 완료 기종만 가능. */
  function buildLine(s, programId) {
    const p = s.programs.find((x) => x.id === programId);
    if (!p || p.phase !== 'production') return { ok: false, error: '양산 가능한 기종이 아닙니다.' };
    const seg = SEGMENTS[p.segment];
    if (s.cash < seg.lineCost) return { ok: false, error: `라인 건설비 ${fmtMoney(seg.lineCost)}이 부족합니다.` };
    s.cash -= seg.lineCost;
    s.lines.push({
      id: 'line-' + s.nextId++,
      programId: p.id,
      capacity: seg.lineMaxRate,
      ramp: 0.15,
      partial: 0,
      idle: false,
      builtTurn: s.turn,
    });
    pushLog(s, 'good', `${p.name} 전용 조립 라인 신설 (${fmtMoney(seg.lineCost)}). 최대 분기 ${seg.lineMaxRate}기.`);
    return { ok: true };
  }

  /** 라인 폐쇄 — 건설비의 20%를 회수한다. */
  function closeLine(s, lineId) {
    const idx = s.lines.findIndex((l) => l.id === lineId);
    if (idx < 0) return { ok: false, error: '라인을 찾을 수 없습니다.' };
    const line = s.lines[idx];
    const p = s.programs.find((x) => x.id === line.programId);
    const refund = Math.round(SEGMENTS[p.segment].lineCost * 0.2);
    s.cash += refund;
    s.lines.splice(idx, 1);
    pushLog(s, 'info', `${p.name} 라인 폐쇄. 설비 매각으로 ${fmtMoney(refund)} 회수.`);
    return { ok: true };
  }

  /**
   * 라인 가동 중지/재개. 수주 잔고가 없는데 계속 찍어내면 화이트테일 재고가
   * 현금을 태우므로, 멈춰 세우는 것도 중요한 경영 판단이다.
   * 재가동 시 램프업을 처음부터 다시 올려야 한다.
   */
  function toggleLine(s, lineId) {
    const line = s.lines.find((l) => l.id === lineId);
    if (!line) return { ok: false, error: '라인을 찾을 수 없습니다.' };
    line.idle = !line.idle;
    const p = s.programs.find((x) => x.id === line.programId);
    if (line.idle) {
      line.ramp = 0.15;
      line.partial = 0;
      pushLog(s, 'info', `${p ? p.name : '라인'} 조립 라인 가동 중지. 재가동 시 램프업을 다시 올려야 한다.`);
    } else {
      pushLog(s, 'info', `${p ? p.name : '라인'} 조립 라인 가동 재개.`);
    }
    return { ok: true };
  }

  /** 미인도 재고(화이트테일) 헐값 처분 — 정가의 68%. */
  function sellStock(s, programId, qty) {
    const p = s.programs.find((x) => x.id === programId);
    if (!p || p.stock <= 0) return { ok: false, error: '처분할 재고가 없습니다.' };
    const n = Math.min(qty, p.stock);
    const revenue = Math.round(n * p.listPrice * 0.68);
    p.stock -= n;
    p.delivered += n;
    s.cash += revenue;
    s.stats.delivered += n;
    s.stats.revenue += revenue;
    // 분기 중 발생한 처분 실적 — 다음 정산 리포트에 합산해, 현금은 늘었는데
    // 재무표의 매출·손익·인도에는 빠져 설명이 안 되는 상태를 막는다.
    s.pending.revenue += revenue;
    s.pending.delivered += n;
    adjustReputation(s, -1);
    pushLog(s, 'info', `${p.name} 재고 ${n}기를 리스사에 정가 68%로 처분. ${fmtMoney(revenue)} 확보.`);
    return { ok: true };
  }

  function hireEngineers(s, count) {
    const cost = Math.round(Math.abs(count) * CONFIG.engineerHireCost);
    if (count > 0) {
      if (s.cash < cost) return { ok: false, error: `채용 비용 ${fmtMoney(cost)}이 부족합니다.` };
      s.cash -= cost;
      s.engineers += count;
      pushLog(s, 'info', `엔지니어 ${count.toLocaleString('ko-KR')}명 채용 (${fmtMoney(cost)}).`);
    } else {
      const cut = Math.min(-count, s.engineers - 500);
      if (cut <= 0) return { ok: false, error: '최소 인력 500명은 유지해야 합니다.' };
      s.cash -= Math.round(cut * CONFIG.engineerHireCost * 0.5); // 퇴직 위로금
      s.engineers -= cut;
      adjustReputation(s, -1);
      pushLog(s, 'bad', `엔지니어 ${cut.toLocaleString('ko-KR')}명 감원. 조직이 술렁인다.`);
    }
    return { ok: true };
  }

  function borrow(s, amount) {
    const room = CONFIG.maxDebt - s.debt;
    const take = Math.min(amount, room);
    if (take <= 0) return { ok: false, error: '차입 한도가 남아있지 않습니다.' };
    s.debt += take;
    s.cash += take;
    pushLog(s, 'info', `${fmtMoney(take)} 차입. 총 부채 ${fmtMoney(s.debt)}.`);
    return { ok: true };
  }

  function repay(s, amount) {
    const pay = Math.min(amount, s.debt, s.cash);
    if (pay <= 0) return { ok: false, error: '상환할 수 있는 금액이 없습니다.' };
    s.debt -= pay;
    s.cash -= pay;
    pushLog(s, 'info', `${fmtMoney(pay)} 상환. 잔여 부채 ${fmtMoney(s.debt)}.`);
    return { ok: true };
  }

  /** RFP 입찰 등록/해제. programId가 null이면 포기. */
  function setBid(s, rfpId, programId, discount) {
    if (!programId) {
      delete s.bids[rfpId];
      return { ok: true };
    }
    s.bids[rfpId] = { programId, discount: clamp(discount, 0, CONFIG.maxDiscount) };
    return { ok: true };
  }

  function adjustReputation(s, delta) {
    s.reputation = clamp(s.reputation + delta, 0, 100);
  }

  // ─────────────────────────────── 분기 정산 ───────────────────────────────

  function endTurn(s) {
    if (s.gameOver) return { ok: false, error: '게임이 종료되었습니다.' };
    // 스키마가 늘어난 뒤 저장된 옛 상태 방어 (필드가 없으면 기본값으로).
    if (!s.effects.grounded) s.effects.grounded = {};
    if (!s.pending) s.pending = { revenue: 0, delivered: 0 };
    const rng = rngFor(s);
    const report = {
      label: turnLabel(s.turn),
      revenue: s.pending.revenue,
      productionCost: 0,
      rdCost: 0,
      overhead: 0,
      interest: 0,
      delivered: s.pending.delivered,
      ordersWon: 0,
    };
    s.pending = { revenue: 0, delivered: 0 };

    resolveBids(s, rng, report);
    advanceDevelopment(s, rng, report);
    runProduction(s, report);
    runDeliveries(s, report);
    settleFinance(s, report);

    s.history.push({
      turn: s.turn,
      label: report.label,
      cash: Math.round(s.cash),
      debt: Math.round(s.debt),
      revenue: Math.round(report.revenue),
      cost: Math.round(report.productionCost + report.rdCost + report.overhead + report.interest),
      net: Math.round(report.revenue - report.productionCost - report.rdCost - report.overhead - report.interest),
      delivered: report.delivered,
      backlog: totalBacklog(s),
      reputation: Math.round(s.reputation),
    });
    if (s.history.length > 120) s.history.shift();

    // 파산은 정산 결과로 확정한다. 다음 분기 이벤트를 먼저 굴리면 연구지원금 같은
    // 현금 유입이 이미 지급불능인 회사를 되살려 "즉시 종료" 규칙과 어긋난다.
    if (checkBankrupt(s)) {
      saveRng(s, rng);
      return { ok: true, report };
    }

    // ── 다음 분기로 ──
    s.turn++;

    // 마지막 분기를 정산했다면 여기서 끝낸다. 존재하지 않는 다음 분기의 경쟁사 인도량이
    // 최종 점유율을 깎거나, 이벤트가 최종 현금·평판까지 바꾼다.
    if (s.turn >= CONFIG.totalTurns) {
      finishGame(s);
      saveRng(s, rng);
      return { ok: true, report };
    }

    tickEffects(s);
    driftMarket(s, rng);
    simulateRivals(s, rng);
    s.events = rollEvents(s, rng);
    s.rfps = generateRfps(s, rng);
    s.bids = {};

    saveRng(s, rng);
    return { ok: true, report };
  }

  function resolveBids(s, rng, report) {
    // 1단계: 분기 시작 상태로 모든 입찰 점수를 먼저 고정한다.
    // 순차 처리하면 앞선 수주로 오른 평판·관계가 뒤 입찰의 점수를 바꿔,
    // 플레이어가 화면에서 확인한 점수와 다른 값으로 판정된다.
    const pending = [];
    for (const rfp of s.rfps) {
      const bid = s.bids[rfp.id];
      if (!bid) {
        // 무응찰은 관계가 서서히 식는다.
        s.relations[rfp.airlineId] = clamp((s.relations[rfp.airlineId] ?? 40) - 1.5, 0, 100);
        continue;
      }
      const program = s.programs.find((p) => p.id === bid.programId);
      if (!program || program.phase !== 'production') continue;

      const score = scoreBid(s, rfp, program, bid.discount);
      if (score.blocked) continue;
      pending.push({ rfp, program, score });
    }

    // 2단계: 고정된 점수로 판정하고 보상을 적용한다.
    for (const { rfp, program, score } of pending) {
      s.stats.bidsMade++;
      const result = resolveBid(s, rfp, { score }, rng);
      s.relations[rfp.airlineId] = clamp((s.relations[rfp.airlineId] ?? 40) + 2, 0, 100);

      if (result.outcome === 'lose') {
        pushLog(
          s,
          'bad',
          `${rfp.airlineName} ${rfp.qty}기 수주 실패 — ${result.rivalName}에 밀렸다. (우리 ${score.total} vs ${result.rivalScore})`,
        );
        continue;
      }

      const unitPrice = score.price;
      const deposit = Math.round(result.qty * unitPrice * CONFIG.depositRate);
      s.cash += deposit;
      report.revenue += deposit;
      report.ordersWon += result.qty;
      s.stats.ordersWon += result.qty;
      s.relations[rfp.airlineId] = clamp(s.relations[rfp.airlineId] + 10, 0, 100);
      adjustReputation(s, result.outcome === 'win' ? 2 : 1);

      s.backlog.push({
        id: 'ord-' + s.nextId++,
        airlineId: rfp.airlineId,
        airlineName: rfp.airlineName,
        programId: program.id,
        programName: program.name,
        qty: result.qty,
        remaining: result.qty,
        unitPrice,
        wonTurn: s.turn,
      });

      const verb = result.outcome === 'win' ? '단독 수주' : '분할 수주';
      pushLog(
        s,
        'good',
        `${rfp.airlineName} ${verb}! ${program.name} ${result.qty}기, 대당 ${fmtMoney(unitPrice)} (총 ${fmtMoney(result.qty * unitPrice)}). 선수금 ${fmtMoney(deposit)} 입금.`,
      );
    }
  }

  function advanceDevelopment(s, rng, report) {
    // 이번 분기 시작 시점에 이미 인증 심사 중이던 프로그램 (아래 카운트다운 대상).
    const certifyingBefore = s.programs.filter((p) => p.phase === 'cert');

    // 인력 배분 0% = 프로그램 동결. 진행도 그대로 멈추고 개발비도 나가지 않는다.
    // 현금이 마를 때 개발을 갈아엎지 않고 버티는 유일한 탈출구다.
    const active = s.programs.filter((p) => p.phase === 'dev' && p.share > 0);
    const totalShare = active.reduce((a, p) => a + p.share, 0);

    for (const p of active) {
      const allocated = s.engineers * (p.share / totalShare);
      const ratio = allocated / p.engineersNeeded;
      const effective = Math.min(1.4, ratio);
      const gain = (100 / p.devQuarters) * effective;

      // 인력을 과도하게 밀어넣으면 설계 검증이 얕아진다.
      if (ratio > 1.25 && rng.chance(0.35)) {
        p.defectRisk = Math.round(Math.min(0.6, p.defectRisk * 1.08) * 1000) / 1000;
      }

      const before = p.progress;
      p.progress = Math.min(100, p.progress + gain);
      // 착수금으로 이미 낸 몫을 빼고 남은 개발비만 진행도에 비례해 집행한다.
      // (전액을 다시 배분하면 실제 지출이 표시된 총 개발비의 108%가 된다.)
      const spend = p.devCost * (1 - CONFIG.launchUpfrontRate) * ((p.progress - before) / 100);
      p.spent += spend;
      s.cash -= spend;
      report.rdCost += spend;

      if (p.progress >= 100) {
        p.phase = 'cert';
        pushLog(s, 'program', `${p.name} 설계 동결 및 초도 비행 성공. 형식증명 심사에 들어간다 (예상 ${p.certRemaining}분기).`);
      }
    }

    // 개발 루프에서 방금 cert로 전환된 프로그램은 제외한다. 포함하면 개발에 그 분기를
    // 다 쓰고도 인증 1분기가 함께 지나가, 3분기짜리 인증이 실제로는 2분기에 끝난다.
    for (const p of certifyingBefore) {
      p.certRemaining -= 1;
      if (p.certRemaining <= 0) {
        p.phase = 'production';
        p.certTurn = s.turn;
        adjustReputation(s, 5);
        pushLog(
          s,
          'good',
          `${p.name} 형식증명 취득! 이제 조립 라인을 세워 양산할 수 있다. (정가 ${fmtMoney(p.listPrice)}, 연비지수 ${p.efficiency})`,
        );
      }
    }
  }

  function runProduction(s, report) {
    let mult = 1;
    if (s.effects.strikeQuarters > 0) mult *= 0.5;
    if (s.effects.supplyQuarters > 0) mult *= 0.75;

    // 기종별 미인도 주문 잔량. 라인은 이 범위 + 소량의 선행 생산까지만 만든다.
    // (이 상한이 없으면 주문이 없어도 계속 찍어내 화이트테일이 무한히 쌓인다.)
    const ordered = {};
    for (const o of s.backlog) {
      if (o.remaining > 0) ordered[o.programId] = (ordered[o.programId] || 0) + o.remaining;
    }

    for (const line of s.lines) {
      const p = s.programs.find((x) => x.id === line.programId);
      if (!p || p.phase !== 'production' || line.idle) continue;

      // 미인도 주문에서 이미 쌓아둔 재고를 뺀 만큼만 만든다. p.stock이 갱신되므로
      // 같은 기종에 라인이 여러 개여도 자연히 나눠 갖는다.
      // 주문을 넘어선 선행 생산은 허용하지 않는다 — 여유분을 두면 재고를 처분할 때마다
      // 그만큼이 매 분기 재생성돼, 원가보다 비싼 처분가로 무한히 현금을 찍을 수 있다.
      const headroom = (ordered[p.id] || 0) - p.stock;
      if (headroom <= 0) {
        // 만들 게 없으면 라인이 식는다 — 재가동 시 램프업을 다시 올려야 한다.
        line.ramp = Math.max(0.15, line.ramp - CONFIG.rampPerQuarter * 0.5);
        line.partial = 0;
        continue;
      }

      line.ramp = Math.min(1, line.ramp + CONFIG.rampPerQuarter);
      const raw = line.capacity * line.ramp * mult + line.partial;
      let units = Math.floor(raw);
      line.partial = raw - units;
      if (units > headroom) {
        units = headroom;
        line.partial = 0;
      }
      if (units <= 0) continue;

      let cost = 0;
      for (let i = 0; i < units; i++) {
        p.produced++;
        cost += unitCostAt(p.unitCostBase, p.produced);
      }
      s.cash -= cost;
      report.productionCost += cost;
      p.stock += units;
    }
  }

  function runDeliveries(s, report) {
    // 오래된 수주부터 인도한다.
    const orders = s.backlog.filter((o) => o.remaining > 0).sort((a, b) => a.wonTurn - b.wonTurn);
    for (const o of orders) {
      const p = s.programs.find((x) => x.id === o.programId);
      if (!p || p.stock <= 0) continue;
      if ((s.effects.grounded[p.id] || 0) > 0) continue;

      const n = Math.min(o.remaining, p.stock);
      const revenue = n * o.unitPrice * (1 - CONFIG.depositRate);
      o.remaining -= n;
      p.stock -= n;
      p.delivered += n;
      s.cash += revenue;
      s.stats.delivered += n;
      s.stats.revenue += revenue;
      report.revenue += revenue;
      report.delivered += n;

      if (o.remaining === 0) {
        adjustReputation(s, 1);
        pushLog(s, 'good', `${o.airlineName} ${o.programName} ${o.qty}기 인도 완료. 잔금 정산.`);
      }
    }
  }

  function settleFinance(s, report) {
    const rate = CONFIG.interestPerQuarter + (s.effects.rateBumpQuarters > 0 ? s.effects.rateBump : 0);
    const interest = s.debt * rate;
    const overhead =
      CONFIG.fixedOverheadPerQuarter +
      s.lines.length * CONFIG.lineOverheadPerLine +
      s.engineers * CONFIG.engineerCostPerQuarter +
      s.programs.reduce((a, p) => a + p.stock * p.unitCostBase * CONFIG.inventoryHoldingCost, 0);

    s.cash -= interest + overhead;
    report.interest += interest;
    report.overhead += overhead;

    // 현금이 마르면 한도까지 자동 차입 — 파산은 한도까지 쓴 뒤에 온다.
    if (s.cash < 0) {
      const need = Math.ceil(-s.cash);
      const room = CONFIG.maxDebt - s.debt;
      const take = Math.min(need, room);
      if (take > 0) {
        s.debt += take;
        s.cash += take;
        pushLog(s, 'bad', `운전자금 부족으로 ${fmtMoney(take)} 긴급 차입. 총 부채 ${fmtMoney(s.debt)}.`);
      }
    }
  }

  function tickEffects(s) {
    const e = s.effects;
    if (e.strikeQuarters > 0) e.strikeQuarters--;
    if (e.supplyQuarters > 0) e.supplyQuarters--;
    if (e.rateBumpQuarters > 0) e.rateBumpQuarters--;
    for (const id of Object.keys(e.grounded)) {
      e.grounded[id] -= 1;
      if (e.grounded[id] <= 0) {
        delete e.grounded[id];
        const p = s.programs.find((x) => x.id === id);
        if (p) pushLog(s, 'good', `${p.name} 운항 정지 해제. 인도를 재개한다.`);
      }
    }
  }

  function driftMarket(s, rng) {
    const m = s.market;
    // 평균 회귀 + 잡음. 시장은 늘 1.0 근처로 되돌아가려 한다.
    m.fuelIndex = clamp(m.fuelIndex + (1 - m.fuelIndex) * 0.12 + rng.normal(0, 0.05), 0.45, 2.2);
    m.demandIndex = clamp(m.demandIndex + (1 - m.demandIndex) * 0.15 + rng.normal(0, 0.06), 0.35, 2.0);
    s.reputation = clamp(s.reputation + (50 - s.reputation) * 0.03, 0, 100);
  }

  /** 경쟁사 인도량을 추상적으로 굴려 시장 점유율을 만든다. */
  function simulateRivals(s, rng) {
    const industry = Math.max(4, Math.round(22 * s.market.demandIndex + rng.normal(0, 3)));
    s.stats.rivalDelivered += industry;
  }

  function rollEvents(s, rng) {
    const fired = [];
    // 분기당 0~2건. 초반 몇 분기는 조용하게 둔다.
    if (s.turn < 3) return fired;
    let draws = rng.chance(0.55) ? 1 : 0;
    if (rng.chance(0.15)) draws++;

    for (let i = 0; i < draws; i++) {
      const pool = EVENTS.filter((e) => !e.condition || e.condition(s));
      if (!pool.length) break;
      const totalW = pool.reduce((a, e) => a + e.weight, 0);
      let r = rng.next() * totalW;
      const chosen = pool.find((e) => (r -= e.weight) <= 0) || pool[0];

      const helpers = {
        rng,
        fmt: fmtMoney,
        reputation: (d) => adjustReputation(s, d),
        /** 가중 추첨 — 결함 대상 선정처럼 "위험이 높을수록 자주 걸린다"를 표현할 때. */
        pickWeighted: (arr, weightOf) => {
          const w = arr.map((x) => Math.max(1e-4, weightOf(x)));
          const total = w.reduce((a, b) => a + b, 0);
          let r = rng.next() * total;
          for (let i = 0; i < arr.length; i++) {
            r -= w[i];
            if (r <= 0) return arr[i];
          }
          return arr[arr.length - 1];
        },
      };
      const text = chosen.apply(s, helpers);
      fired.push({ id: chosen.id, name: chosen.name, text });
      pushLog(s, 'event', `[${chosen.name}] ${text}`);
    }
    return fired;
  }

  /** 지급불능 판정 — 종료됐으면 true. */
  function checkBankrupt(s) {
    if (s.gameOver) return true;
    if (s.cash < 0 && s.debt >= CONFIG.maxDebt) {
      s.gameOver = { reason: 'bankrupt', ...finalScore(s, true) };
      pushLog(s, 'bad', '자금이 완전히 고갈되고 차입 한도도 소진됐다. 회사는 법정관리에 들어간다.');
      return true;
    }
    return false;
  }

  function finishGame(s) {
    if (s.gameOver) return;
    s.gameOver = { reason: 'complete', ...finalScore(s, false) };
    pushLog(s, 'info', '20년의 경영이 끝났다. 최종 성적을 정산한다.');
  }

  // ─────────────────────────────── 파생 지표 ───────────────────────────────

  function totalBacklog(s) {
    return s.backlog.reduce((a, o) => a + o.remaining, 0);
  }

  function backlogValue(s) {
    return s.backlog.reduce((a, o) => a + o.remaining * o.unitPrice * (1 - CONFIG.depositRate), 0);
  }

  function marketShare(s) {
    const total = s.stats.delivered + s.stats.rivalDelivered;
    return total > 0 ? s.stats.delivered / total : 0;
  }

  function netWorth(s) {
    const assetValue =
      s.programs.reduce((a, p) => a + p.stock * p.unitCostBase, 0) +
      s.lines.reduce((a, l) => {
        const p = s.programs.find((x) => x.id === l.programId);
        return a + (p ? SEGMENTS[p.segment].lineCost * 0.4 : 0);
      }, 0);
    return s.cash + assetValue - s.debt;
  }

  function finalScore(s, bankrupt) {
    const share = marketShare(s);
    const worth = netWorth(s);
    const score = Math.round(
      s.stats.delivered * 1.2 + share * 4000 + Math.max(0, worth) * 0.08 + s.reputation * 12,
    );
    // 파산은 아무리 많이 팔았어도 실패다 — 등급으로 성적을 덮지 않는다.
    let grade = 'F';
    if (!bankrupt) {
      grade = 'D';
      if (score >= 5200) grade = 'S';
      else if (score >= 3600) grade = 'A';
      else if (score >= 2400) grade = 'B';
      else if (score >= 1400) grade = 'C';
    }
    return { score, grade, share, worth, delivered: s.stats.delivered };
  }

  /**
   * 현재 인력 배분 기준으로 개발 완료까지 남은 분기 수.
   * 설계 시 표시되는 "예상 N분기"는 인력이 100% 충족됐을 때의 값이라,
   * 실제로는 훨씬 오래 걸릴 수 있다. 그 간극을 플레이어에게 정직하게 보여준다.
   */
  function projectedQuarters(s, p) {
    if (p.phase !== 'dev') return 0;
    if (p.share <= 0) return Infinity; // 동결
    const active = s.programs.filter((x) => x.phase === 'dev' && x.share > 0);
    const totalShare = active.reduce((a, x) => a + x.share, 0);
    if (!totalShare) return Infinity;
    const allocated = s.engineers * (p.share / totalShare);
    const effective = Math.min(1.4, allocated / p.engineersNeeded);
    if (effective <= 0) return Infinity;
    const gain = (100 / p.devQuarters) * effective;
    return Math.ceil((100 - p.progress) / gain);
  }

  /** 현재 열린 RFP에 대해 입찰 가능한 기종 목록 */
  function eligiblePrograms(s, rfp) {
    return s.programs
      .filter((p) => p.phase === 'production' && p.segment === rfp.segment)
      .map((p) => ({ program: p, score: scoreBid(s, rfp, p, s.bids[rfp.id]?.discount ?? 0) }));
  }

  root.AirlinerEngine = {
    newGame,
    launchProgram,
    derivativeSpec,
    investQuality,
    cancelProgram,
    buildLine,
    closeLine,
    toggleLine,
    sellStock,
    hireEngineers,
    borrow,
    repay,
    setBid,
    endTurn,
    eligiblePrograms,
    totalBacklog,
    backlogValue,
    marketShare,
    netWorth,
    finalScore,
    projectedQuarters,
    fmtMoney,
    turnLabel,
  };
})(typeof globalThis !== 'undefined' ? globalThis : this);
