// MSW for Node (tests). Start in a test's beforeAll: server.listen().
import { setupServer } from "msw/node";
import { handlers } from "./handlers";

export const server = setupServer(...handlers);
