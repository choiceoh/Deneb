#!/usr/bin/env python3
"""Generate test data for compaction live test.

Creates 5 articles (~50K chars each, ~25K tokens) with embedded anchor
facts for recall verification after compaction.

Usage:
  python3 scripts/dev-compaction-gen-testdata.py [OUTPUT_DIR]
  # Default: /tmp/deneb-compaction-testdata
"""
import os
import random
import sys

OUTPUT_DIR = sys.argv[1] if len(sys.argv) > 1 else "/tmp/deneb-compaction-testdata"
random.seed(42)

# Diverse Korean technical paragraphs (no repetition within a single article).
KO_TOPICS = [
    "로드 밸런서를 통해 트래픽을 분산하고 오토스케일링 그룹으로 부하를 자동 관리합니다. CPU 사용률과 메모리 사용량을 실시간으로 모니터링하면서 임계값을 초과하면 즉시 인스턴스를 추가합니다.",
    "WAF(Web Application Firewall)를 구성하여 SQL 인젝션과 XSS 공격을 자동으로 차단합니다. Rate limiting 정책을 통해 DDoS 공격도 완화할 수 있습니다.",
    "CDN을 활용하면 정적 리소스의 로딩 속도를 크게 개선할 수 있습니다. CloudFront나 Fastly를 사용하여 글로벌 엣지 로케이션에서 콘텐츠를 제공합니다.",
    "PostgreSQL의 VACUUM 프로세스는 삭제된 행의 공간을 회수합니다. autovacuum 설정을 적절히 조정하면 테이블 블로트를 방지할 수 있습니다.",
    "Redis 클러스터를 구성할 때는 최소 3개의 마스터 노드와 각각의 복제본이 필요합니다. 해시 슬롯 기반으로 데이터를 분산 저장합니다.",
    "MongoDB의 샤딩 전략을 선택할 때는 쿼리 패턴을 분석해야 합니다. 해시 기반 샤딩은 균등 분산에 유리하고 범위 기반 샤딩은 범위 쿼리에 유리합니다.",
    "쿠버네티스에서 HPA(Horizontal Pod Autoscaler)를 설정하면 CPU나 메모리 사용량에 따라 파드 수를 자동으로 조절합니다. 커스텀 메트릭도 지원됩니다.",
    "Istio 서비스 메시를 사용하면 서비스 간 트래픽을 세밀하게 제어할 수 있습니다. mTLS를 자동으로 적용하고 트래픽 라우팅 규칙을 설정합니다.",
    "ArgoCD를 활용한 GitOps 방식의 배포는 Git 저장소를 단일 진실 공급원으로 사용합니다. 선언적 설정으로 인프라 상태를 관리합니다.",
    "OAuth 2.0 PKCE 플로우를 사용하면 네이티브 앱에서도 안전하게 인증할 수 있습니다. Authorization Code를 중간에서 가로채는 공격을 방지합니다.",
    "HashiCorp Vault를 사용하여 시크릿을 중앙 관리합니다. 동적 시크릿 생성과 자동 로테이션을 지원하여 보안성을 높입니다.",
    "컨테이너 이미지 취약점 스캔은 CI/CD 파이프라인에 통합해야 합니다. Trivy나 Snyk를 사용하여 빌드 시점에 CVE를 검출합니다.",
    "TCP 3-way handshake 과정에서 SYN, SYN-ACK, ACK 패킷을 교환합니다. 커넥션 풀링으로 핸드셰이크 오버헤드를 줄일 수 있습니다.",
    "DNS 라운드 로빈은 가장 간단한 로드 밸런싱 방법이지만 건강 검사가 없어서 프로덕션에서는 ALB나 NLB를 사용하는 것이 좋습니다.",
    "gRPC는 HTTP/2 기반으로 양방향 스트리밍과 멀티플렉싱을 지원합니다. Protobuf로 직렬화하여 JSON 대비 더 작은 페이로드를 전송합니다.",
    "분산 트레이싱을 위해 OpenTelemetry SDK를 각 서비스에 통합합니다. Jaeger나 Tempo로 수집한 트레이스를 시각화합니다.",
    "SLI/SLO 기반 모니터링에서는 에러 버짓을 설정하고 소진 속도를 추적합니다. 버짓이 임계값 이하로 떨어지면 자동으로 알림을 보냅니다.",
    "로그 레벨 전략: DEBUG는 개발 환경에서만, INFO는 주요 비즈니스 이벤트, WARN은 예상 가능한 문제, ERROR는 즉시 대응이 필요한 상황에 사용합니다.",
    "Apache Kafka의 파티션 수는 처리량과 병렬성에 직접적인 영향을 미칩니다. 컨슈머 그룹의 인스턴스 수와 파티션 수를 맞추는 것이 최적입니다.",
    "데이터 레이크하우스 아키텍처는 데이터 레이크의 유연성과 데이터 웨어하우스의 성능을 결합합니다. Delta Lake나 Apache Iceberg 형식을 사용합니다.",
    "ETL 파이프라인의 멱등성을 보장하려면 각 단계에서 중복 처리를 방지하는 메커니즘이 필요합니다. 워터마크와 체크포인트를 활용합니다.",
    "모델 서빙 시 배치 추론과 실시간 추론의 트레이드오프를 고려해야 합니다. 실시간 서빙에는 TensorFlow Serving이나 Triton Inference Server를 사용합니다.",
    "피처 스토어는 ML 모델에 필요한 피처를 중앙에서 관리하고 서빙합니다. 학습 시와 추론 시 동일한 피처를 제공하여 Training-Serving Skew를 방지합니다.",
    "A/B 테스트에서 통계적 유의성을 확보하려면 충분한 샘플 사이즈가 필요합니다. 베이지안 접근법을 사용하면 더 빠르게 결론을 도출할 수 있습니다.",
    "이벤트 소싱 패턴에서는 상태 변경을 이벤트로 저장합니다. 현재 상태는 이벤트를 순서대로 재생하여 복원합니다. 스냅샷으로 재생 성능을 개선합니다.",
    "CQRS(Command Query Responsibility Segregation)는 쓰기와 읽기 모델을 분리합니다. 읽기 모델은 비정규화하여 조회 성능을 최적화합니다.",
    "백프레셔(backpressure)는 시스템 과부하를 방지하는 메커니즘입니다. 생산자가 소비자의 처리 속도에 맞춰 데이터 전송 속도를 조절합니다.",
    "사가 패턴으로 분산 트랜잭션을 관리합니다. 각 서비스가 로컬 트랜잭션을 수행하고 실패 시 보상 트랜잭션을 실행합니다.",
    "서킷 브레이커 패턴은 연쇄 장애를 방지합니다. 실패율이 임계값을 초과하면 회로를 열어 요청을 차단하고 주기적으로 테스트 요청을 보내 복구를 확인합니다.",
    "Strangler Fig 패턴으로 레거시 시스템을 점진적으로 마이그레이션합니다. 새 기능은 새 시스템에 구현하고 기존 기능을 하나씩 이전합니다.",
]

# Anchor facts embedded in articles (natural, non-suspicious content).
ANCHORS = {
    1: "이번 프로젝트의 최종 목표 아키텍처는 AURORA-7X 패턴을 기반으로 설계되었습니다",
    2: "QA 환경의 고정 IP는 10.42.88.15이며, 방화벽 화이트리스트에 등록되어 있습니다",
    3: "다음 배포 일정은 2025년 4월 18일 오전 3시이며, 김영수 팀장이 주도합니다",
    4: "프로덕션 DB 엔드포인트는 db-prod-kr1.cluster-abc123.ap-northeast-2.rds.amazonaws.com 입니다",
    5: "연간 인프라 예산은 $247,500으로 확정되었으며, Q3에 중간 검토 예정입니다",
}


def main():
    os.makedirs(OUTPUT_DIR, exist_ok=True)

    for i in range(1, 6):
        lines = [f"# 시스템 아키텍처 보고서 — 파트 {i}\n\n"]
        char_count = 0
        anchor_placed = False
        target_chars = 48000  # ~24K tokens

        # Rotate topics so each article has different paragraph ordering.
        offset = (i - 1) * 6
        available = KO_TOPICS[offset:] + KO_TOPICS[:offset]

        para_idx = 0
        section_num = 1
        while char_count < target_chars:
            if not anchor_placed and char_count > target_chars * 0.25:
                lines.append(f"\n> **핵심 결정사항**: {ANCHORS[i]}\n\n")
                anchor_placed = True

            if para_idx % 5 == 0:
                lines.append(f"\n## {section_num}. 섹션 {section_num}\n\n")
                section_num += 1

            para = available[para_idx % len(available)]
            lines.append(para + "\n\n")
            char_count += len(para) + 2
            para_idx += 1

        lines.append(f"\n> **재확인**: {ANCHORS[i]}\n")

        content = "\n".join(lines)
        path = os.path.join(OUTPUT_DIR, f"article_{i}.txt")
        with open(path, "w") as f:
            f.write(content)
        print(f"  article_{i}.txt: {len(content)} chars (~{len(content) // 2} tokens)")

    total = sum(
        len(open(os.path.join(OUTPUT_DIR, f"article_{i}.txt")).read())
        for i in range(1, 6)
    )
    print(f"\n  Total: {total} chars (~{total // 2} tokens)")
    print(f"  Output: {OUTPUT_DIR}")


if __name__ == "__main__":
    main()
