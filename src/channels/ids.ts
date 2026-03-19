/**
 * Deneb: 채널 ID는 동적 레지스트리에서 관리.
 * ChatChannelId 타입을 string으로 완화하여 새 채널 추가 시 코어 수정 불필요.
 * 
 * 하위 호환: 기존 코드에서 ChatChannelId를 참조하는 곳은 동작함.
 */

// 더 이상 고정 목록이 아님. 동적 레지스트리에서 런타임에 결정.
// getChatChannelOrder()를 사용하여 현재 등록된 채널 목록을 가져올 것.
export type ChatChannelId = string;

export const CHANNEL_IDS: string[] = []; // 런타임에 populate됨
