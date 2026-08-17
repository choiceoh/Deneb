/*
 * 시드 고정 난수기 — 저장/복원과 테스트 재현성을 위해 Math.random 대신 사용한다.
 * 상태(a)가 정수 하나뿐이라 게임 세이브에 그대로 직렬화된다.
 */
(function (root) {
  'use strict';

  function createRng(seed) {
    let a = (seed >>> 0) || 1;

    function next() {
      a = (a + 0x6d2b79f5) >>> 0;
      let t = a;
      t = Math.imul(t ^ (t >>> 15), t | 1);
      t ^= t + Math.imul(t ^ (t >>> 7), t | 61);
      return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
    }

    return {
      next,
      /** [min, max) 실수 */
      range: (min, max) => min + next() * (max - min),
      /** [min, max] 정수 */
      int: (min, max) => Math.floor(min + next() * (max - min + 1)),
      pick: (arr) => arr[Math.floor(next() * arr.length)],
      chance: (p) => next() < p,
      /** Box-Muller 근사 — 이벤트 강도/경쟁사 입찰 흔들기에 사용 */
      normal(mean, sd) {
        const u = Math.max(next(), 1e-9);
        const v = next();
        return mean + sd * Math.sqrt(-2 * Math.log(u)) * Math.cos(2 * Math.PI * v);
      },
      /** 세이브용 상태 추출/주입 */
      getState: () => a,
      setState: (s) => {
        a = (s >>> 0) || 1;
      },
    };
  }

  root.AirlinerRng = { createRng };
})(typeof globalThis !== 'undefined' ? globalThis : this);
