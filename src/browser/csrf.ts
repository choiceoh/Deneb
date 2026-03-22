// Stub: CSRF protection removed for solo-dev simplification.
import type { NextFunction, Request, Response } from "express";

export function shouldRejectBrowserMutation(_params: {
  method: string;
  origin?: string;
  referer?: string;
  secFetchSite?: string;
}): boolean {
  return false;
}

export function browserMutationGuardMiddleware(): (
  req: Request,
  res: Response,
  next: NextFunction,
) => void {
  return (_req: Request, _res: Response, next: NextFunction) => {
    next();
  };
}
