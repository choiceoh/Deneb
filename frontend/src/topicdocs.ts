// topicdocs.ts — client for miniapp.topicdocs.* (per-forum-topic knowledge
// files under <workspace>/topics/*.md). Distinct from topics.ts, which targets
// miniapp.topics.* (Telegram forum topic creation).

import { call } from './rpc';

export interface TopicFile {
  name: string;
  size: number;
  modified: string; // RFC3339, "" when unknown
}

export interface TopicFileContent {
  name: string;
  content: string;
  size: number;
  modified: string;
}

export interface TopicWriteResult {
  name: string;
  size: number;
  modified: string;
  created: boolean;
}

export const listTopicFiles = (initData: string) =>
  call<{ files: TopicFile[] }>('miniapp.topicdocs.list_files', null, initData);

export const readTopicFile = (initData: string, name: string) =>
  call<TopicFileContent>('miniapp.topicdocs.read_file', { name }, initData);

export const writeTopicFile = (
  initData: string,
  name: string,
  content: string,
  create = false,
) =>
  call<TopicWriteResult>(
    'miniapp.topicdocs.write_file',
    { name, content, create },
    initData,
  );
