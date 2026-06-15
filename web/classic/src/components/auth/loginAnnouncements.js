/*
Copyright (C) 2025 QuantumNous

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

const getStatusValue = (status, key) => status?.[key] ?? status?.data?.[key];

export const getLoginAnnouncements = (status) => {
  const enabled = getStatusValue(status, 'announcements_enabled');
  const announcements = getStatusValue(status, 'announcements');
  const visibleAnnouncements =
    enabled !== false && Array.isArray(announcements)
      ? announcements
          .filter((item) => {
            if (!item || typeof item !== 'object') return false;
            return (
              typeof item.content === 'string' && item.content.trim().length > 0
            );
          })
          .slice(0, 20)
      : [];

  if (visibleAnnouncements.length > 0) return visibleAnnouncements;

  const legacyNotice = getStatusValue(status, 'notice');
  if (typeof legacyNotice !== 'string' || legacyNotice.trim().length === 0) {
    return [];
  }

  return [
    {
      id: 0,
      content: legacyNotice,
      type: 'default',
    },
  ];
};
