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

const IMAGE_PLAYGROUND_PATH = '/image-playground'

export function isImagePlaygroundPath(pathname: string): boolean {
  return (
    pathname === IMAGE_PLAYGROUND_PATH ||
    pathname === `${IMAGE_PLAYGROUND_PATH}/`
  )
}

export function shouldKeepImagePlaygroundMounted(
  hasMounted: boolean,
  active: boolean
): boolean {
  return hasMounted || active
}
