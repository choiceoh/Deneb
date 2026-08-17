/*
 * 실존 제조사와 그들이 1998~2017년에 실제로 팔던 여객기 카탈로그.
 *
 * 경쟁사 경쟁력을 고정 스칼라로 두지 않고 "그 시점에 실제로 시장에 있던 기종"에서
 * 유도한다. 그래서 1998년의 협동체 문턱은 737-800/A320이고, 2016년에는 A320neo다.
 *
 * 주의 — power 는 플레이어 입찰 점수(0~100)와 **같은 척도**다. 올리면 그 시장이
 * 통째로 닫힌다. 기종을 추가·수정하면 반드시 밸런스 시뮬레이션을 다시 돌릴 것.
 */
(function (root) {
  'use strict';

  /** 1998~2017 사이 실제로 여객기를 인도한 제조사들. */
  const MANUFACTURERS = [
    { id: 'boeing', name: '보잉', latin: 'Boeing' },
    { id: 'airbus', name: '에어버스', latin: 'Airbus' },
    { id: 'embraer', name: '엠브라에르', latin: 'Embraer' },
    { id: 'bombardier', name: '봉바르디에', latin: 'Bombardier' },
    { id: 'atr', name: 'ATR', latin: 'ATR' },
    { id: 'bae', name: 'BAE 시스템스', latin: 'BAE Systems' },
    { id: 'tupolev', name: '투폴레프', latin: 'Tupolev' },
    { id: 'sukhoi', name: '수호이', latin: 'Sukhoi' },
  ];

  /**
   * 기종 카탈로그.
   *   segment : 이 게임의 세그먼트 분류 (좌석수가 아니라 시장 위치 기준 — 757-300은
   *             243석이지만 협동체다)
   *   seats   : 전형적 2클래스 좌석수
   *   range   : 최대 항속 (km)
   *   eis     : 취항 시점, 소수 연도 (1998.5 = 1998년 3분기)
   *   end     : 신규 판매 종료 시점. null 이면 2017년까지 계속 팔았다
   *   power   : 입찰 점수 척도(0~100)에서의 기본 경쟁력
   *   eff     : 연비 지수. 1.0 = 1998년 협동체(737-800) 기준. 유가가 오르면 격차가 벌어진다
   */
  const AIRCRAFT = [
    // ── 보잉 (1997년 맥도널 더글러스 합병분 포함) ──
    { id: 'b737-400', maker: 'boeing', name: '737-400', segment: 'narrow', seats: 146, range: 4000, eis: 1988, end: 2000, power: 44, eff: 0.88 },
    { id: 'b737-600', maker: 'boeing', name: '737-600', segment: 'regional', seats: 108, range: 5650, eis: 1998.5, end: 2007, power: 47, eff: 0.97 },
    { id: 'b737-700', maker: 'boeing', name: '737-700', segment: 'narrow', seats: 126, range: 6230, eis: 1998, end: null, power: 55, eff: 0.99 },
    { id: 'b737-800', maker: 'boeing', name: '737-800', segment: 'narrow', seats: 162, range: 5400, eis: 1998.25, end: null, power: 58, eff: 1.0 },
    { id: 'b737-900er', maker: 'boeing', name: '737-900ER', segment: 'narrow', seats: 180, range: 5900, eis: 2007.25, end: null, power: 60, eff: 1.02 },
    { id: 'b737max8', maker: 'boeing', name: '737 MAX 8', segment: 'narrow', seats: 178, range: 6570, eis: 2017.25, end: null, power: 66, eff: 1.14 },
    { id: 'b717', maker: 'boeing', name: '717-200', segment: 'regional', seats: 106, range: 2650, eis: 1999.75, end: 2006.5, power: 46, eff: 0.9 },
    { id: 'md83', maker: 'boeing', name: 'MD-83', segment: 'narrow', seats: 155, range: 4600, eis: 1985, end: 2000, power: 40, eff: 0.8 },
    { id: 'md90', maker: 'boeing', name: 'MD-90', segment: 'narrow', seats: 153, range: 3860, eis: 1995, end: 2000.5, power: 43, eff: 0.86 },
    { id: 'b757-200', maker: 'boeing', name: '757-200', segment: 'narrow', seats: 200, range: 7250, eis: 1983, end: 2005, power: 53, eff: 0.9 },
    { id: 'b757-300', maker: 'boeing', name: '757-300', segment: 'narrow', seats: 243, range: 6290, eis: 1999.25, end: 2005, power: 52, eff: 0.92 },
    { id: 'b767-200er', maker: 'boeing', name: '767-200ER', segment: 'wide', seats: 181, range: 12200, eis: 1984, end: 2004, power: 50, eff: 0.9 },
    { id: 'b767-300er', maker: 'boeing', name: '767-300ER', segment: 'wide', seats: 218, range: 11300, eis: 1988, end: null, power: 54, eff: 0.93 },
    { id: 'b767-400er', maker: 'boeing', name: '767-400ER', segment: 'wide', seats: 245, range: 10400, eis: 2000.75, end: 2012, power: 56, eff: 0.95 },
    { id: 'b777-200er', maker: 'boeing', name: '777-200ER', segment: 'wide', seats: 301, range: 13080, eis: 1997, end: 2014, power: 60, eff: 1.0 },
    { id: 'b777-300', maker: 'boeing', name: '777-300', segment: 'wide', seats: 368, range: 11120, eis: 1998.5, end: 2007, power: 58, eff: 0.97 },
    { id: 'b777-300er', maker: 'boeing', name: '777-300ER', segment: 'wide', seats: 365, range: 13650, eis: 2004.25, end: null, power: 64, eff: 1.05 },
    { id: 'b777-200lr', maker: 'boeing', name: '777-200LR', segment: 'wide', seats: 301, range: 15840, eis: 2006, end: null, power: 62, eff: 1.02 },
    { id: 'b747-400', maker: 'boeing', name: '747-400', segment: 'wide', seats: 416, range: 13450, eis: 1989, end: 2006, power: 55, eff: 0.88 },
    { id: 'b747-8i', maker: 'boeing', name: '747-8I', segment: 'wide', seats: 467, range: 14320, eis: 2012.25, end: null, power: 58, eff: 1.0 },
    { id: 'md11', maker: 'boeing', name: 'MD-11', segment: 'wide', seats: 293, range: 12600, eis: 1990, end: 2001, power: 46, eff: 0.82 },
    { id: 'b787-8', maker: 'boeing', name: '787-8', segment: 'wide', seats: 242, range: 13620, eis: 2011.75, end: null, power: 66, eff: 1.18 },
    { id: 'b787-9', maker: 'boeing', name: '787-9', segment: 'wide', seats: 290, range: 14140, eis: 2014.5, end: null, power: 68, eff: 1.2 },

    // ── 에어버스 ──
    { id: 'a318', maker: 'airbus', name: 'A318', segment: 'regional', seats: 107, range: 5750, eis: 2003.75, end: 2014, power: 48, eff: 0.95 },
    { id: 'a319', maker: 'airbus', name: 'A319', segment: 'narrow', seats: 124, range: 6950, eis: 1996.25, end: null, power: 55, eff: 1.0 },
    { id: 'a320', maker: 'airbus', name: 'A320-200', segment: 'narrow', seats: 150, range: 6150, eis: 1988, end: null, power: 57, eff: 1.0 },
    { id: 'a321', maker: 'airbus', name: 'A321-200', segment: 'narrow', seats: 185, range: 5950, eis: 1997.25, end: null, power: 58, eff: 1.0 },
    { id: 'a320neo', maker: 'airbus', name: 'A320neo', segment: 'narrow', seats: 165, range: 6300, eis: 2016, end: null, power: 66, eff: 1.15 },
    { id: 'a321neo', maker: 'airbus', name: 'A321neo', segment: 'narrow', seats: 206, range: 6850, eis: 2017.25, end: null, power: 67, eff: 1.16 },
    { id: 'a300-600r', maker: 'airbus', name: 'A300-600R', segment: 'wide', seats: 266, range: 7500, eis: 1988, end: 2007.5, power: 44, eff: 0.85 },
    { id: 'a310-300', maker: 'airbus', name: 'A310-300', segment: 'wide', seats: 220, range: 9600, eis: 1986, end: 2007.5, power: 43, eff: 0.85 },
    { id: 'a330-300', maker: 'airbus', name: 'A330-300', segment: 'wide', seats: 277, range: 11750, eis: 1994, end: null, power: 59, eff: 1.0 },
    { id: 'a330-200', maker: 'airbus', name: 'A330-200', segment: 'wide', seats: 247, range: 13400, eis: 1998.25, end: null, power: 60, eff: 1.02 },
    { id: 'a340-300', maker: 'airbus', name: 'A340-300', segment: 'wide', seats: 295, range: 13500, eis: 1993, end: 2011, power: 54, eff: 0.9 },
    { id: 'a340-600', maker: 'airbus', name: 'A340-600', segment: 'wide', seats: 380, range: 14450, eis: 2002.5, end: 2011, power: 56, eff: 0.9 },
    { id: 'a380', maker: 'airbus', name: 'A380-800', segment: 'wide', seats: 525, range: 15200, eis: 2007.75, end: null, power: 62, eff: 1.05 },
    { id: 'a350-900', maker: 'airbus', name: 'A350-900', segment: 'wide', seats: 315, range: 15000, eis: 2015, end: null, power: 68, eff: 1.2 },

    // ── 엠브라에르 ──
    { id: 'erj145', maker: 'embraer', name: 'ERJ 145', segment: 'regional', seats: 50, range: 2870, eis: 1996.75, end: 2016, power: 45, eff: 0.95 },
    { id: 'e170', maker: 'embraer', name: 'E170', segment: 'regional', seats: 76, range: 3900, eis: 2004.25, end: null, power: 58, eff: 1.05 },
    { id: 'e175', maker: 'embraer', name: 'E175', segment: 'regional', seats: 88, range: 3900, eis: 2005.5, end: null, power: 59, eff: 1.05 },
    { id: 'e190', maker: 'embraer', name: 'E190', segment: 'regional', seats: 100, range: 4500, eis: 2005.75, end: null, power: 61, eff: 1.06 },
    { id: 'e195', maker: 'embraer', name: 'E195', segment: 'regional', seats: 116, range: 4260, eis: 2006.75, end: null, power: 60, eff: 1.04 },

    // ── 봉바르디에 ──
    { id: 'crj200', maker: 'bombardier', name: 'CRJ200', segment: 'regional', seats: 50, range: 3150, eis: 1996, end: 2006, power: 44, eff: 0.9 },
    { id: 'crj700', maker: 'bombardier', name: 'CRJ700', segment: 'regional', seats: 70, range: 3600, eis: 2001, end: null, power: 54, eff: 0.98 },
    { id: 'crj900', maker: 'bombardier', name: 'CRJ900', segment: 'regional', seats: 88, range: 2950, eis: 2003, end: null, power: 56, eff: 1.0 },
    { id: 'crj1000', maker: 'bombardier', name: 'CRJ1000', segment: 'regional', seats: 104, range: 3000, eis: 2010.75, end: null, power: 57, eff: 1.0 },
    { id: 'q400', maker: 'bombardier', name: 'Dash 8 Q400', segment: 'regional', seats: 78, range: 2040, eis: 2000, end: null, power: 52, eff: 1.12 },
    { id: 'cs100', maker: 'bombardier', name: 'CS100', segment: 'regional', seats: 108, range: 5460, eis: 2016.5, end: null, power: 65, eff: 1.18 },
    { id: 'cs300', maker: 'bombardier', name: 'CS300', segment: 'regional', seats: 130, range: 6110, eis: 2016.75, end: null, power: 66, eff: 1.18 },

    // ── ATR (터보프롭 — 연비가 무기라 유가가 오르면 존재감이 커진다) ──
    { id: 'atr42-500', maker: 'atr', name: 'ATR 42-500', segment: 'regional', seats: 48, range: 1560, eis: 1995.75, end: null, power: 42, eff: 1.1 },
    { id: 'atr72-500', maker: 'atr', name: 'ATR 72-500', segment: 'regional', seats: 70, range: 1650, eis: 1997.75, end: 2012, power: 48, eff: 1.15 },
    { id: 'atr72-600', maker: 'atr', name: 'ATR 72-600', segment: 'regional', seats: 72, range: 1528, eis: 2011.5, end: null, power: 52, eff: 1.18 },

    // ── BAE 시스템스 (2003년 아브로 RJ 생산 종료로 시장에서 퇴장) ──
    { id: 'rj85', maker: 'bae', name: '아브로 RJ85', segment: 'regional', seats: 85, range: 2200, eis: 1993, end: 2003.5, power: 42, eff: 0.85 },
    { id: 'rj100', maker: 'bae', name: '아브로 RJ100', segment: 'regional', seats: 100, range: 2000, eis: 1993, end: 2003.5, power: 43, eff: 0.85 },

    // ── 투폴레프 · 수호이 ──
    { id: 'tu204', maker: 'tupolev', name: 'Tu-204-100', segment: 'narrow', seats: 210, range: 4600, eis: 1996, end: null, power: 40, eff: 0.85 },
    { id: 'tu214', maker: 'tupolev', name: 'Tu-214', segment: 'narrow', seats: 210, range: 6500, eis: 2001.5, end: null, power: 42, eff: 0.86 },
    { id: 'ssj100', maker: 'sukhoi', name: 'SSJ100', segment: 'regional', seats: 98, range: 3050, eis: 2011.25, end: null, power: 53, eff: 1.08 },
  ];

  const MAKER_BY_ID = Object.fromEntries(MANUFACTURERS.map((m) => [m.id, m]));

  /** 턴 인덱스 → 소수 연도. 0턴 = 1998년 1분기 = 1998.0 */
  function yearAt(turn, startYear) {
    return startYear + turn / 4;
  }

  /** 그 시점에 신규 판매 중이었나. end 는 배타적(그 해부터는 못 산다). */
  function inService(type, year) {
    return year >= type.eis && (type.end === null || year < type.end);
  }

  /** 해당 시점·세그먼트에서 팔리고 있던 기종들. */
  function availableTypes(segment, year) {
    return AIRCRAFT.filter((t) => t.segment === segment && inService(t, year));
  }

  /**
   * 요구사양 대비 적합도 감점. 플레이어가 받는 실격 규칙과 같은 기준을 쓰되,
   * 경쟁사는 "실격" 대신 큰 감점을 받는다 — 안 맞는 기종이라도 값을 깎아 들이밀기 때문.
   * 반대로 요구보다 지나치게 큰 기체도 감점이다(편당 운항비가 안 맞는다).
   */
  function fitPenalty(type, reqSeats, reqRange) {
    let p = 0;

    const rangeRatio = type.range / reqRange;
    if (rangeRatio < 0.9) p += 14 + (0.9 - rangeRatio) * 40;
    else if (rangeRatio < 1) p += (1 - rangeRatio) * 60;

    const seatRatio = type.seats / reqSeats;
    if (seatRatio < 0.8) p += 12 + (0.8 - seatRatio) * 40;
    else if (seatRatio < 1) p += (1 - seatRatio) * 25;
    else if (seatRatio > 1.35) p += Math.min(14, (seatRatio - 1.35) * 22);

    return Math.min(30, p);
  }

  /**
   * 한 기종이 이 공고에서 내는 점수.
   * @param {object} type    기종
   * @param {number} fuel    연료지수 (1.0 = 기준)
   * @param {number} reqSeats 요구 좌석수 (null 이면 적합도 무시 — 시장 개관용)
   * @param {number} reqRange 요구 항속
   */
  function typeScore(type, fuel, reqSeats, reqRange) {
    // 유가가 기준보다 높으면 연비 격차가 그대로 점수 격차가 된다.
    let score = type.power + (type.eff - 1) * 12 * (fuel - 1);
    if (reqSeats && reqRange) score -= fitPenalty(type, reqSeats, reqRange);
    return score;
  }

  root.AirlinerFleet = {
    MANUFACTURERS,
    AIRCRAFT,
    MAKER_BY_ID,
    yearAt,
    inService,
    availableTypes,
    fitPenalty,
    typeScore,
  };
})(typeof globalThis !== 'undefined' ? globalThis : this);
