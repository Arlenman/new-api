/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { loadSessions, saveSessions } from '../storage/storage'
import {
  createPlaygroundSessionRemote,
  getPlaygroundSessions,
  savePlaygroundSessionMessages,
} from '../../api'
import { createImageGenerationTaskManager } from './image-generation-task-manager'

export const imageGenerationTaskManager = createImageGenerationTaskManager({
  getSessions: loadSessions,
  saveSessions,
  saveSessionMessages: (sessionId, messages) => {
    void savePlaygroundSessionMessages(sessionId, messages).catch(async () => {
      const session = loadSessions()?.find((item) => item.id === sessionId)
      if (!session) {
        return
      }
      try {
        await createPlaygroundSessionRemote(session)
        await savePlaygroundSessionMessages(sessionId, messages)
      } catch {
        /* empty */
      }
    })
  },
  recoverImageMessage: async (sessionId, assistantMessageKey) => {
    const sessions = await getPlaygroundSessions()
    saveSessions(sessions)
    const session = sessions.find((item) => item.id === sessionId)
    return (
      session?.messages.find((message) => message.key === assistantMessageKey) ??
      null
    )
  },
})
