import assert from 'node:assert/strict'
import { describe, test } from 'node:test'

import {
  buildAttachmentContextText,
  extractPlaygroundAttachmentText,
  MAX_ATTACHMENT_CONTEXT_CHARS,
  MAX_ATTACHMENT_TEXT_CHARS_PER_FILE,
} from './playground-attachment-text.ts'

function encodeDataUrl(mediaType: string, text: string): string {
  return `data:${mediaType};base64,${Buffer.from(text).toString('base64')}`
}

function createStoredZip(entries: Record<string, string>): Uint8Array {
  const chunks: Uint8Array[] = []
  const centralDirectory: Uint8Array[] = []
  let offset = 0
  let index = 0

  const encoder = new TextEncoder()

  function uint16(value: number): Uint8Array {
    return new Uint8Array([value & 0xff, (value >> 8) & 0xff])
  }

  function uint32(value: number): Uint8Array {
    return new Uint8Array([
      value & 0xff,
      (value >> 8) & 0xff,
      (value >> 16) & 0xff,
      (value >> 24) & 0xff,
    ])
  }

  function concat(parts: Uint8Array[]): Uint8Array {
    const total = parts.reduce((sum, part) => sum + part.length, 0)
    const result = new Uint8Array(total)
    let position = 0
    for (const part of parts) {
      result.set(part, position)
      position += part.length
    }
    return result
  }

  for (const [name, value] of Object.entries(entries)) {
    const nameBytes = encoder.encode(name)
    const data = encoder.encode(value)
    const localHeader = concat([
      uint32(0x04034b50),
      uint16(20),
      uint16(0),
      uint16(0),
      uint16(0),
      uint16(0),
      uint32(0),
      uint32(data.length),
      uint32(data.length),
      uint16(nameBytes.length),
      uint16(0),
      nameBytes,
    ])

    chunks.push(localHeader, data)
    centralDirectory.push(
      concat([
        uint32(0x02014b50),
        uint16(20),
        uint16(20),
        uint16(0),
        uint16(0),
        uint16(0),
        uint16(0),
        uint32(0),
        uint32(data.length),
        uint32(data.length),
        uint16(nameBytes.length),
        uint16(0),
        uint16(0),
        uint16(0),
        uint16(0),
        uint32(0),
        uint32(offset),
        nameBytes,
      ])
    )
    offset += localHeader.length + data.length
    index += 1
  }

  const centralStart = offset
  const central = concat(centralDirectory)
  const end = concat([
    uint32(0x06054b50),
    uint16(0),
    uint16(0),
    uint16(index),
    uint16(index),
    uint32(central.length),
    uint32(centralStart),
    uint16(0),
  ])

  return concat([...chunks, central, end])
}

function createDocxFixture(text: string): Uint8Array {
  return createStoredZip({
    '[Content_Types].xml':
      '<?xml version="1.0" encoding="UTF-8"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types" />',
    'word/document.xml': `<?xml version="1.0" encoding="UTF-8"?>
      <w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
        <w:body>
          <w:p><w:r><w:t>${text}</w:t></w:r></w:p>
        </w:body>
      </w:document>`,
  })
}

function createXlsxFixture(): Uint8Array {
  return createStoredZip({
    '[Content_Types].xml':
      '<?xml version="1.0" encoding="UTF-8"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types" />',
    'xl/sharedStrings.xml': `<?xml version="1.0" encoding="UTF-8"?>
      <sst xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
        <si><t>姓名</t></si>
        <si><t>张三</t></si>
        <si><t>金额</t></si>
        <si><t>100</t></si>
      </sst>`,
    'xl/workbook.xml': `<?xml version="1.0" encoding="UTF-8"?>
      <workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"
        xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships">
        <sheets>
          <sheet name="合同" sheetId="1" r:id="rId1"/>
        </sheets>
      </workbook>`,
    'xl/_rels/workbook.xml.rels': `<?xml version="1.0" encoding="UTF-8"?>
      <Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
        <Relationship Id="rId1"
          Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet"
          Target="worksheets/sheet1.xml"/>
      </Relationships>`,
    'xl/worksheets/sheet1.xml': `<?xml version="1.0" encoding="UTF-8"?>
      <worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main">
        <sheetData>
          <row r="1"><c r="A1" t="s"><v>0</v></c><c r="B1" t="s"><v>2</v></c></row>
          <row r="2"><c r="A2" t="s"><v>1</v></c><c r="B2" t="s"><v>3</v></c></row>
        </sheetData>
      </worksheet>`,
  })
}

function createPdfFixture(text: string): Uint8Array {
  const encoder = new TextEncoder()
  const objects = [
    '1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n',
    '2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n',
    '3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>\nendobj\n',
    '4 0 obj\n<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>\nendobj\n',
    `5 0 obj\n<< /Length ${text.length + 36} >>\nstream\nBT /F1 24 Tf 100 700 Td (${text}) Tj ET\nendstream\nendobj\n`,
  ]
  let pdf = '%PDF-1.4\n'
  const offsets = [0]
  for (const object of objects) {
    offsets.push(encoder.encode(pdf).length)
    pdf += object
  }
  const xrefOffset = encoder.encode(pdf).length
  pdf += `xref\n0 ${objects.length + 1}\n`
  pdf += '0000000000 65535 f \n'
  for (const offset of offsets.slice(1)) {
    pdf += `${String(offset).padStart(10, '0')} 00000 n \n`
  }
  pdf += `trailer\n<< /Size ${objects.length + 1} /Root 1 0 R >>\nstartxref\n${xrefOffset}\n%%EOF\n`

  return encoder.encode(pdf)
}

function toBlobPart(bytes: Uint8Array): ArrayBuffer {
  return bytes.buffer.slice(
    bytes.byteOffset,
    bytes.byteOffset + bytes.byteLength
  ) as ArrayBuffer
}

describe('playground attachment text extraction', () => {
  test('extracts txt, csv and json attachments', async () => {
    const textResult = await extractPlaygroundAttachmentText({
      filename: 'notes.txt',
      mediaType: 'text/plain',
      url: encodeDataUrl('text/plain', 'hello text'),
    })
    const csvResult = await extractPlaygroundAttachmentText({
      filename: 'table.csv',
      mediaType: 'text/csv',
      file: new File(['name,amount\nalice,10'], 'table.csv', {
        type: 'text/csv',
      }),
    })
    const jsonResult = await extractPlaygroundAttachmentText({
      filename: 'data.json',
      mediaType: 'application/json',
      url: encodeDataUrl('application/json', '{"name":"合同"}'),
    })

    assert.equal(textResult.extractionStatus, 'complete')
    assert.equal(textResult.extractedText, 'hello text')
    assert.equal(csvResult.extractionStatus, 'complete')
    assert.match(csvResult.extractedText ?? '', /alice,10/)
    assert.equal(jsonResult.extractionStatus, 'complete')
    assert.match(jsonResult.extractedText ?? '', /合同/)
  })

  test('extracts readable text from PDF, docx and xlsx attachments', async () => {
    const pdfResult = await extractPlaygroundAttachmentText({
      filename: 'contract.pdf',
      mediaType: 'application/pdf',
      file: new File(
        [toBlobPart(createPdfFixture('PDF Contract Text'))],
        'contract.pdf',
        {
          type: 'application/pdf',
        }
      ),
    })
    const docxResult = await extractPlaygroundAttachmentText({
      filename: 'contract.docx',
      mediaType:
        'application/vnd.openxmlformats-officedocument.wordprocessingml.document',
      file: new File(
        [toBlobPart(createDocxFixture('DOCX Contract Text'))],
        'contract.docx'
      ),
    })
    const xlsxResult = await extractPlaygroundAttachmentText({
      filename: 'contract.xlsx',
      mediaType:
        'application/vnd.openxmlformats-officedocument.spreadsheetml.sheet',
      file: new File([toBlobPart(createXlsxFixture())], 'contract.xlsx'),
    })

    assert.equal(pdfResult.extractionStatus, 'complete')
    assert.match(pdfResult.extractedText ?? '', /PDF Contract Text/)
    assert.equal(docxResult.extractionStatus, 'complete')
    assert.match(docxResult.extractedText ?? '', /DOCX Contract Text/)
    assert.equal(xlsxResult.extractionStatus, 'complete')
    assert.match(xlsxResult.extractedText ?? '', /合同/)
    assert.match(xlsxResult.extractedText ?? '', /张三/)
  })

  test('marks empty extracted content as empty instead of silently submitting it', async () => {
    const result = await extractPlaygroundAttachmentText({
      filename: 'empty.txt',
      mediaType: 'text/plain',
      file: new File(['   '], 'empty.txt', { type: 'text/plain' }),
    })

    assert.equal(result.extractionStatus, 'empty')
    assert.equal(result.error, '未提取到可读文本')
  })

  test('truncates oversized per-file and total attachment context text', () => {
    const longText = 'a'.repeat(MAX_ATTACHMENT_TEXT_CHARS_PER_FILE + 100)
    const context = buildAttachmentContextText([
      {
        filename: 'long.txt',
        mediaType: 'text/plain',
        extractedText: longText,
        extractionStatus: 'complete',
      },
      {
        filename: 'another.txt',
        mediaType: 'text/plain',
        extractedText: 'b'.repeat(MAX_ATTACHMENT_CONTEXT_CHARS),
        extractionStatus: 'complete',
      },
    ])

    assert.ok(context.length <= MAX_ATTACHMENT_CONTEXT_CHARS + 200)
    assert.match(context, /long\.txt/)
    assert.match(context, /内容已截断/)
  })

  test('returns an unsupported status for binary files', async () => {
    const result = await extractPlaygroundAttachmentText({
      filename: 'archive.bin',
      mediaType: 'application/octet-stream',
      file: new File([new Uint8Array([1, 2, 3])], 'archive.bin'),
    })

    assert.equal(result.extractionStatus, 'unsupported')
    assert.match(result.error ?? '', /不支持/)
  })
})
