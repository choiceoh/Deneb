/*
 * 기체 설계 계산기 — 설계 입력(좌석/항속/기술/소재)을 개발비·개발기간·성능으로 환산한다.
 * 순수 함수만 둔다. UI의 실시간 미리보기와 engine의 실제 착수가 같은 식을 쓰도록.
 */
(function (root) {
  'use strict';

  const { SEGMENTS, MATERIALS, CONFIG } = root.AirlinerData;

  const clamp = (v, lo, hi) => Math.min(hi, Math.max(lo, v));

  /** 파생형 할인 허용 오차 — 이 범위를 벗어나면 사실상 새 기체다. */
  const DERIVATIVE_TOLERANCE = { techUp: 5, rangeRatio: 0.15 };

  /**
   * 원형의 형식증명을 물려받을 수 있는 변경인지.
   * 좌석수 변경(동체 연장/단축)은 파생형의 본령이라 허용하고,
   * 소재 교체·기술 상향·항속 대폭 변경은 재설계에 가까우므로 할인 대상이 아니다.
   */
  function isCompatibleDerivative(spec, range, tech) {
    const d = spec.derivedFrom;
    if (!d) return false;
    // 원형 정보가 없는 옛 설계안은 보수적으로 할인하지 않는다.
    if (d.material === undefined || d.tech === undefined || d.range === undefined) return false;
    if (d.material !== spec.material) return false;
    if (tech > d.tech + DERIVATIVE_TOLERANCE.techUp) return false;
    if (Math.abs(range - d.range) > d.range * DERIVATIVE_TOLERANCE.rangeRatio) return false;
    return true;
  }

  /**
   * 설계안 평가.
   * @param {{segment:string, seats:number, range:number, tech:number, material:string, derivedFrom?:object}} spec
   * @returns 개발비/기간/성능이 채워진 평가 결과
   */
  function evaluate(spec) {
    const seg = SEGMENTS[spec.segment];
    const mat = MATERIALS[spec.material];
    if (!seg || !mat) throw new Error('알 수 없는 세그먼트/소재: ' + spec.segment + '/' + spec.material);

    const seats = clamp(spec.seats, seg.seats.min, seg.seats.max);
    const range = clamp(spec.range, seg.range.min, seg.range.max);
    const tech = clamp(spec.tech, 0, 100);

    const seatRatio = seats / seg.seats.ref;
    const rangeRatio = range / seg.range.ref;

    // 개발비: 좌석은 초선형(대형화가 비싸다), 항속은 완만, 기술은 강하게 작용.
    let devCost =
      seg.devBase *
      Math.pow(seatRatio, 1.15) *
      Math.pow(rangeRatio, 0.55) *
      (1 + (tech / 100) * 0.9) *
      mat.devCostMult;

    let devQuarters =
      seg.devQuarters * (1 + (tech / 100) * 0.35) * Math.pow(rangeRatio, 0.12) * mat.devTimeMult;

    let engineersNeeded = seg.engineersNeeded * Math.pow(seatRatio, 0.5) * (1 + (tech / 100) * 0.4);

    // 파생형: 기존 형식증명을 물려받아 개발비·기간이 크게 준다.
    // 단, 원형의 형식증명을 실제로 재사용할 수 있는 변경일 때만 인정한다.
    // 딱지만 붙인 채 소재·기술·항속을 갈아엎으면 신규 설계를 34% 가격에 사는 셈이라
    // 개발비 제약 자체가 무너진다 (동일 설계 기준 $18.6B → $6.3B).
    const derivative = isCompatibleDerivative(spec, range, tech);
    if (derivative) {
      devCost *= 0.34;
      devQuarters *= 0.5;
      engineersNeeded *= 0.55;
    }

    // 연비 지수(0~100). 기술 투자 + 소재가 좌우하고, 과도한 항속은 구조중량으로 깎인다.
    const rangePenalty = Math.max(0, rangeRatio - 1) * 9;
    const efficiency = clamp(22 + tech * 0.62 + mat.efficiencyBonus - rangePenalty, 5, 99);

    // 객실 쾌적성 — 항공사 프리미엄 노선 평가에 반영.
    const comfort = clamp(38 + tech * 0.28 + mat.comfortBonus, 10, 99);

    // 표준 생산원가: 크기·항속에 비례, 기술/소재가 올린다.
    const unitCostBase =
      seg.unitCostBase *
      Math.pow(seatRatio, 0.92) *
      Math.pow(rangeRatio, 0.3) *
      (1 + (tech / 100) * 0.3) *
      mat.unitCostMult;

    // 정가: 원가가 아니라 "시장이 값을 쳐주는 가치" 기준으로 만든다.
    const listPrice =
      seg.listPriceBase *
      Math.pow(seatRatio, 0.95) *
      Math.pow(rangeRatio, 0.34) *
      (1 + (tech / 100) * 0.42) *
      (1 + mat.efficiencyBonus / 130);

    // 개발 리스크: 기술을 밀어붙이고 복합재를 쓸수록 결함 확률이 오른다.
    const defectRisk = clamp(0.05 + (tech / 100) * 0.22 + mat.riskBonus, 0.03, 0.6);

    return {
      segment: seg.id,
      seats: Math.round(seats),
      range: Math.round(range),
      tech: Math.round(tech),
      material: mat.id,
      devCost: Math.round(devCost),
      devQuarters: Math.max(2, Math.round(devQuarters)),
      engineersNeeded: Math.round(engineersNeeded),
      efficiency: Math.round(efficiency),
      comfort: Math.round(comfort),
      unitCostBase: Math.round(unitCostBase * 10) / 10,
      listPrice: Math.round(listPrice * 10) / 10,
      defectRisk: Math.round(defectRisk * 1000) / 1000,
      certQuarters: seg.certQuarters,
      // UI가 "파생형 할인이 적용됐는지"를 그대로 보여줄 수 있게 노출한다.
      derivative,
    };
  }

  /**
   * 누적 생산 n번째 기체의 실제 원가 — 학습곡선(87%)을 적용한다.
   * 초기 기체는 표준원가를 크게 웃돌아, 램프업 구간에서 적자가 나는 게 정상이다.
   */
  function unitCostAt(unitCostBase, cumulativeUnits) {
    const n = Math.max(1, cumulativeUnits);
    const factor = Math.max(CONFIG.learningFloor, Math.pow(n, CONFIG.learningExponent));
    return unitCostBase * CONFIG.firstUnitPremium * factor;
  }

  /** 설계 기본값 — 세그먼트를 고를 때마다 슬라이더를 이 값으로 되돌린다. */
  function defaultSpec(segmentId) {
    const seg = SEGMENTS[segmentId];
    return {
      segment: segmentId,
      seats: seg.seats.ref,
      range: seg.range.ref,
      tech: 50,
      material: 'aluminum',
    };
  }

  root.AirlinerDesign = { evaluate, unitCostAt, defaultSpec, clamp, isCompatibleDerivative };
})(typeof globalThis !== 'undefined' ? globalThis : this);
