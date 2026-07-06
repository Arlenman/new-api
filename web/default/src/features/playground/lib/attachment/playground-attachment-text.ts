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
import { unzipSync } from 'fflate'

import type {
  PlaygroundAttachmentExtractionStatus,
  PlaygroundImageFile,
} from '../../types.ts'

export const MAX_ATTACHMENT_TEXT_CHARS_PER_FILE = 40_000
export const MAX_ATTACHMENT_CONTEXT_CHARS = 100_000

const EMPTY_TEXT_ERROR = '未提取到可读文本'
const UNSUPPORTED_FILE_ERROR = '不支持的附件类型'
const PARSE_FAILED_ERROR = '附件解析失败'

type AttachmentKind = 'image' | 'pdf' | 'docx' | 'xlsx' | 'text' | 'unsupported'
type ZipEntries = Record<string, Uint8Array>
type PdfJsModule = typeof import('pdfjs-dist/legacy/build/pdf.mjs')

export type PlaygroundAttachmentExtractionInput = PlaygroundImageFile & {
  file?: Blob
}

function getFileExtension(filename?: string): string {
  const match = filename?.toLowerCase().match(/\.([^.]+)$/)
  return match?.[1] ?? ''
}

function getAttachmentKind(
  attachment: PlaygroundAttachmentExtractionInput
): AttachmentKind {
  const mediaType = attachment.mediaType?.toLowerCase() ?? ''
  const extension = getFileExtension(attachment.filename)

  if (mediaType.startsWith('image/')) {
    return 'image'
  }

  if (mediaType === 'application/pdf' || extension === 'pdf') {
    return 'pdf'
  }

  if (
    mediaType ===
      'application/vnd.openxmlformats-officedocument.wordprocessingml.document' ||
    extension === 'docx'
  ) {
    return 'docx'
  }

  if (
    mediaType ===
      'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet' ||
    extension === 'xlsx'
  ) {
    return 'xlsx'
  }

  if (
    mediaType.startsWith('text/') ||
    mediaType === 'application/json' ||
    mediaType === 'application/markdown' ||
    ['txt', 'csv', 'json', 'md', 'markdown'].includes(extension)
  ) {
    return 'text'
  }

  return 'unsupported'
}

export function isImageAttachment(attachment: PlaygroundImageFile): boolean {
  return Boolean(attachment.mediaType?.toLowerCase().startsWith('image/'))
}

function decodeBase64(value: string): Uint8Array {
  if (typeof atob === 'function') {
    const binary = atob(value)
    return Uint8Array.from(binary, (char) => char.charCodeAt(0))
  }

  return Uint8Array.from(Buffer.from(value, 'base64'))
}

function decodeDataUrl(dataUrl: string): Uint8Array {
  const commaIndex = dataUrl.indexOf(',')
  if (commaIndex === -1) {
    throw new Error('Invalid data URL')
  }

  const metadata = dataUrl.slice(0, commaIndex)
  const data = dataUrl.slice(commaIndex + 1)

  if (metadata.includes(';base64')) {
    return decodeBase64(data)
  }

  return new TextEncoder().encode(decodeURIComponent(data))
}

async function readAttachmentBytes(
  attachment: PlaygroundAttachmentExtractionInput
): Promise<Uint8Array> {
  if (attachment.file) {
    return new Uint8Array(await attachment.file.arrayBuffer())
  }

  if (!attachment.url) {
    throw new Error('Missing attachment data')
  }

  if (attachment.url.startsWith('data:')) {
    return decodeDataUrl(attachment.url)
  }

  const response = await fetch(attachment.url)
  if (!response.ok) {
    throw new Error(`Failed to fetch attachment: ${response.status}`)
  }

  return new Uint8Array(await response.arrayBuffer())
}

function readText(bytes: Uint8Array): string {
  return new TextDecoder('utf-8', { fatal: false }).decode(bytes)
}

function normalizeExtractedText(text: string): string {
  return text
    .replaceAll('\u0000', '')
    .replaceAll(/[ \t]+\n/g, '\n')
    .replaceAll(/\n{3,}/g, '\n\n')
    .trim()
}

function xmlDecode(value: string): string {
  return value
    .replaceAll('&lt;', '<')
    .replaceAll('&gt;', '>')
    .replaceAll('&quot;', '"')
    .replaceAll('&apos;', "'")
    .replaceAll('&amp;', '&')
}

function getXmlAttribute(xml: string, name: string): string | undefined {
  const match = xml.match(new RegExp(`\\s${name}="([^"]*)"`, 'i'))
  return match ? xmlDecode(match[1]) : undefined
}

function getZipText(entries: ZipEntries, path: string): string | null {
  const bytes = entries[path]
  return bytes ? readText(bytes) : null
}

function extractTextTags(xml: string): string {
  const values: string[] = []
  const matches = xml.matchAll(
    /<(?:[^:>]+:)?t\b[^>]*>([\s\S]*?)<\/(?:[^:>]+:)?t>/gi
  )

  for (const match of matches) {
    values.push(xmlDecode(match[1]))
  }

  return values.join('')
}

function extractDocxText(bytes: Uint8Array): string {
  const entries = unzipSync(bytes)
  const documentXml = getZipText(entries, 'word/document.xml')

  if (!documentXml) {
    throw new Error('Missing word/document.xml')
  }

  const paragraphs: string[] = []
  const paragraphMatches = documentXml.matchAll(
    /<w:p\b[\s\S]*?<\/w:p>|<p\b[\s\S]*?<\/p>/gi
  )

  for (const paragraphMatch of paragraphMatches) {
    const paragraph = extractTextTags(paragraphMatch[0])
    if (paragraph.trim()) {
      paragraphs.push(paragraph)
    }
  }

  return paragraphs.length > 0
    ? paragraphs.join('\n')
    : extractTextTags(documentXml)
}

function parseSharedStrings(entries: ZipEntries): string[] {
  const sharedStringsXml = getZipText(entries, 'xl/sharedStrings.xml')
  if (!sharedStringsXml) {
    return []
  }

  const strings: string[] = []
  const matches = sharedStringsXml.matchAll(/<si\b[\s\S]*?<\/si>/gi)

  for (const match of matches) {
    strings.push(extractTextTags(match[0]))
  }

  return strings
}

type WorkbookSheet = {
  name: string
  path: string
}

function normalizeWorksheetPath(target: string): string {
  if (target.startsWith('/')) {
    return target.slice(1)
  }

  if (target.startsWith('xl/')) {
    return target
  }

  return `xl/${target}`
}

function parseWorkbookSheets(entries: ZipEntries): WorkbookSheet[] {
  const workbookXml = getZipText(entries, 'xl/workbook.xml')
  const relsXml = getZipText(entries, 'xl/_rels/workbook.xml.rels')

  if (!workbookXml || !relsXml) {
    return Object.keys(entries)
      .filter(
        (path) => path.startsWith('xl/worksheets/') && path.endsWith('.xml')
      )
      .sort()
      .map((path, index) => ({
        name: `Sheet${index + 1}`,
        path,
      }))
  }

  const relationships = new Map<string, string>()
  const relationshipMatches = relsXml.matchAll(/<Relationship\b[^>]*>/gi)
  for (const match of relationshipMatches) {
    const id = getXmlAttribute(match[0], 'Id')
    const target = getXmlAttribute(match[0], 'Target')
    if (id && target) {
      relationships.set(id, normalizeWorksheetPath(target))
    }
  }

  const sheets: WorkbookSheet[] = []
  const sheetMatches = workbookXml.matchAll(/<sheet\b[^>]*>/gi)
  for (const match of sheetMatches) {
    const name =
      getXmlAttribute(match[0], 'name') ?? `Sheet${sheets.length + 1}`
    const relationshipId = getXmlAttribute(match[0], 'r:id')
    const path = relationshipId ? relationships.get(relationshipId) : undefined

    if (path) {
      sheets.push({ name, path })
    }
  }

  return sheets
}

function getCellValue(cellXml: string, sharedStrings: string[]): string {
  const cellType = getXmlAttribute(cellXml, 't')

  if (cellType === 'inlineStr') {
    return extractTextTags(cellXml)
  }

  const value = cellXml.match(/<v>([\s\S]*?)<\/v>/i)?.[1]
  if (value === undefined) {
    return ''
  }

  if (cellType === 's') {
    return sharedStrings[Number(value)] ?? ''
  }

  return xmlDecode(value)
}

function extractWorksheetText(
  sheetName: string,
  worksheetXml: string,
  sharedStrings: string[]
): string {
  const rows: string[] = []
  const rowMatches = worksheetXml.matchAll(/<row\b[\s\S]*?<\/row>/gi)

  for (const rowMatch of rowMatches) {
    const cells: string[] = []
    const cellMatches = rowMatch[0].matchAll(/<c\b[\s\S]*?<\/c>/gi)

    for (const cellMatch of cellMatches) {
      cells.push(getCellValue(cellMatch[0], sharedStrings))
    }

    const rowText = cells.join('\t').trim()
    if (rowText) {
      rows.push(rowText)
    }
  }

  return rows.length > 0 ? [`## ${sheetName}`, ...rows].join('\n') : ''
}

function extractXlsxText(bytes: Uint8Array): string {
  const entries = unzipSync(bytes)
  const sharedStrings = parseSharedStrings(entries)
  const sheets = parseWorkbookSheets(entries)
  const sheetTexts: string[] = []

  for (const sheet of sheets) {
    const worksheetXml = getZipText(entries, sheet.path)
    if (!worksheetXml) {
      continue
    }

    const sheetText = extractWorksheetText(
      sheet.name,
      worksheetXml,
      sharedStrings
    )
    if (sheetText) {
      sheetTexts.push(sheetText)
    }
  }

  return sheetTexts.join('\n\n')
}

async function extractPdfText(bytes: Uint8Array): Promise<string> {
  const pdfjs = await loadPdfJs()
  const documentOptions: Record<string, unknown> = {
    data: new Uint8Array(bytes),
    useWorkerFetch: false,
    isEvalSupported: false,
  }

  const nodeStandardFontDataUrl = getNodeStandardFontDataUrl()
  if (nodeStandardFontDataUrl) {
    documentOptions.standardFontDataUrl = nodeStandardFontDataUrl
  }

  const loadingTask = pdfjs.getDocument(documentOptions)
  const document = await loadingTask.promise
  const pages: string[] = []

  try {
    for (let pageNumber = 1; pageNumber <= document.numPages; pageNumber++) {
      const page = await document.getPage(pageNumber)
      const textContent = await page.getTextContent()
      const pageText = textContent.items
        .map((item) => ('str' in item ? item.str : ''))
        .join(' ')
        .trim()

      if (pageText) {
        pages.push(pageText)
      }
    }
  } finally {
    await loadingTask.destroy?.()
    document.cleanup?.()
  }

  return pages.join('\n\n')
}

async function loadPdfJs(): Promise<PdfJsModule> {
  if (typeof window !== 'undefined' && typeof Worker !== 'undefined') {
    return import('pdfjs-dist/legacy/webpack.mjs') as Promise<PdfJsModule>
  }

  return import('pdfjs-dist/legacy/build/pdf.mjs')
}

function getNodeStandardFontDataUrl(): string | undefined {
  if (typeof process === 'undefined' || !process.versions?.node) {
    return undefined
  }

  const sourceUrl = import.meta.url
  const marker = '/web/default/src/'
  const markerIndex = sourceUrl.indexOf(marker)

  if (!sourceUrl.startsWith('file://') || markerIndex === -1) {
    return undefined
  }

  const repoPath = decodeURIComponent(
    sourceUrl.slice('file://'.length, markerIndex)
  )

  return `${repoPath}/web/node_modules/pdfjs-dist/standard_fonts/`
}

function truncateText(text: string, maxLength: number, note: string): string {
  if (text.length <= maxLength) {
    return text
  }

  return `${text.slice(0, Math.max(0, maxLength - note.length))}${note}`
}

function withExtractionResult(
  attachment: PlaygroundAttachmentExtractionInput,
  extractionStatus: PlaygroundAttachmentExtractionStatus,
  extractedText?: string,
  error?: string
): PlaygroundImageFile {
  const { file: _file, ...safeAttachment } = attachment

  return {
    ...safeAttachment,
    extractedText,
    extractionStatus,
    error,
  }
}

function getFailureResult(
  attachment: PlaygroundAttachmentExtractionInput,
  extractionStatus: PlaygroundAttachmentExtractionStatus,
  error: string
): PlaygroundImageFile {
  const { file: _file, ...safeAttachment } = attachment

  return {
    ...safeAttachment,
    extractionStatus,
    error,
  }
}

export async function extractPlaygroundAttachmentText(
  attachment: PlaygroundAttachmentExtractionInput
): Promise<PlaygroundImageFile> {
  const kind = getAttachmentKind(attachment)

  if (kind === 'image' || kind === 'unsupported') {
    return getFailureResult(
      attachment,
      'unsupported',
      kind === 'image' ? '图片附件不需要文本解析' : UNSUPPORTED_FILE_ERROR
    )
  }

  try {
    const bytes = await readAttachmentBytes(attachment)
    let extractedText = ''

    if (kind === 'pdf') {
      extractedText = await extractPdfText(bytes)
    } else if (kind === 'docx') {
      extractedText = extractDocxText(bytes)
    } else if (kind === 'xlsx') {
      extractedText = extractXlsxText(bytes)
    } else {
      extractedText = readText(bytes)
    }

    const normalizedText = normalizeExtractedText(extractedText)

    if (!normalizedText) {
      return getFailureResult(attachment, 'empty', EMPTY_TEXT_ERROR)
    }

    const truncatedText = truncateText(
      normalizedText,
      MAX_ATTACHMENT_TEXT_CHARS_PER_FILE,
      `\n\n[内容已截断，已保留前 ${MAX_ATTACHMENT_TEXT_CHARS_PER_FILE} 字符。]`
    )

    return withExtractionResult(attachment, 'complete', truncatedText)
  } catch (error) {
    return getFailureResult(
      attachment,
      'error',
      error instanceof Error
        ? error.message || PARSE_FAILED_ERROR
        : PARSE_FAILED_ERROR
    )
  }
}

export function stripTransientAttachmentFields(
  attachment: PlaygroundAttachmentExtractionInput
): PlaygroundImageFile {
  const { file: _file, ...safeAttachment } = attachment
  return safeAttachment
}

export function hasExtractedAttachmentText(
  attachment: PlaygroundImageFile
): boolean {
  return (
    attachment.extractionStatus === 'complete' &&
    Boolean(attachment.extractedText?.trim())
  )
}

export function buildAttachmentContextText(
  attachments: PlaygroundImageFile[] = []
): string {
  const blocks = attachments
    .filter(hasExtractedAttachmentText)
    .map((attachment, index) => {
      const title = attachment.filename || `attachment-${index + 1}`
      const mediaType = attachment.mediaType || 'application/octet-stream'
      const text = truncateText(
        attachment.extractedText?.trim() ?? '',
        MAX_ATTACHMENT_TEXT_CHARS_PER_FILE,
        `\n\n[内容已截断，已保留前 ${MAX_ATTACHMENT_TEXT_CHARS_PER_FILE} 字符。]`
      )

      return [`### ${title}`, `类型：${mediaType}`, '', text].join('\n')
    })

  if (blocks.length === 0) {
    return ''
  }

  const context = ['附件内容：', '', ...blocks].join('\n')

  return truncateText(
    context,
    MAX_ATTACHMENT_CONTEXT_CHARS,
    `\n\n[附件内容已截断，已保留前 ${MAX_ATTACHMENT_CONTEXT_CHARS} 字符。]`
  )
}
