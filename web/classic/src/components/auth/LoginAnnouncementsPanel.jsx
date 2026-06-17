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

import React from 'react';
import { useTranslation } from 'react-i18next';
import { marked } from 'marked';
import { formatDateTimeString } from '../../helpers';
import { getLoginAnnouncements } from './loginAnnouncements';

const getAnnouncementTime = (publishDate) => {
  if (!publishDate) return '';

  const date = new Date(publishDate);
  if (isNaN(date.getTime())) return '';

  return formatDateTimeString(date);
};

const LoginAnnouncements = ({ status }) => {
  const { t } = useTranslation();
  const announcements = getLoginAnnouncements(status);

  if (announcements.length === 0) return null;

  return (
    <section
      aria-label={t('系统公告')}
      data-login-announcements
      className='login-announcements-panel rounded-xl border border-semi-color-border bg-semi-color-bg-1 px-4 py-4 text-left shadow-sm'
    >
      <div className='login-announcements-content space-y-3 overflow-y-auto pr-1 card-content-scroll'>
        {announcements.map((item, index) => {
          const time = getAnnouncementTime(item.publishDate);
          const htmlContent = marked.parse(item.content || '');
          const htmlExtra = item.extra ? marked.parse(item.extra) : '';
          const key = item.id || `${item.publishDate || ''}-${index}`;

          return (
            <article
              key={key}
              className={`min-w-0 text-sm leading-6 text-semi-color-text-0 ${index > 0 ? 'border-t border-semi-color-border pt-3' : ''}`}
            >
              <div
                className='break-words'
                dangerouslySetInnerHTML={{ __html: htmlContent }}
              />

              {item.extra && (
                <div
                  className='mt-1 break-words text-xs leading-5 text-semi-color-text-2'
                  dangerouslySetInnerHTML={{ __html: htmlExtra }}
                />
              )}

              {time && (
                <time className='mt-1 block text-xs text-semi-color-text-2'>
                  {time}
                </time>
              )}
            </article>
          );
        })}
      </div>
    </section>
  );
};

export default LoginAnnouncements;
