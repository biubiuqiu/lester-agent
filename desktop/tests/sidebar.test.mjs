import test from 'node:test';
import assert from 'node:assert/strict';
import { buildSidebar } from '../renderer/sidebar.js';

const project = (id, name = id) => ({ id, name });
const task = (id, project_id, extra = {}) => ({ id, project_id, title: id, updated_at: '2026-11-01', ...extra });

test('project trees do not mix device and cloud conversations with identical project IDs', () => {
  const result = buildSidebar({ projects: [project('p')], cloudProjects: [project('p')], conversations: [task('local', 'p')], cloudConversations: [task('cloud', 'p'), task('other-device', 'p', { source: 'local', device_name: 'Laptop' })] });
  assert.deepEqual(result.groups.map(group => [group.key, group.conversations.map(item => item.id)]), [['local:p', ['local']], ['cloud:p', ['cloud']]]);
  assert.deepEqual(result.ungrouped.map(item => [item.id, item._source]), [['other-device', 'remote']]);
});

test('cloud directory copies of a local task do not create duplicate rows', () => {
  const result = buildSidebar({ projects: [project('p')], conversations: [task('same', 'p')], cloudConversations: [task('same', 'p', { source: 'local' })] });
  assert.equal(result.total, 1);
  assert.equal(result.groups[0].total, 1);
  assert.equal(result.ungrouped.length, 0);
});

test('searching a project reveals its conversations; searching a title retains its parent', () => {
  const input = { projects: [project('p', 'Lester desktop'), project('q', 'Notes')], conversations: [task('Design', 'p'), task('Review', 'p'), task('Other', 'q')] };
  assert.deepEqual(buildSidebar({ ...input, query: '  DESKTOP  ' }).groups[0].conversations.map(item => item.id), ['Design', 'Review']);
  const result = buildSidebar({ ...input, query: 'review' });
  assert.deepEqual(result.groups.map(group => group.key), ['local:p']);
  assert.deepEqual(result.groups[0].conversations.map(item => item.id), ['Review']);
  assert.equal(result.groups[0].total, 2);
});

test('tasks with missing projects remain accessible rather than disappearing', () => {
  const result = buildSidebar({ projects: [project('empty')], conversations: [task('orphan', 'missing', { project_name: 'Archived work' })], query: 'archived' });
  assert.equal(result.groups.length, 0);
  assert.equal(result.ungrouped[0].id, 'orphan');
});

test('recent order is deterministic and invalid dates do not hide conversations', () => {
  const result = buildSidebar({ projects: [project('p')], conversations: [task('old', 'p'), task('invalid', 'p', { updated_at: 'bad' }), task('latest', 'p', { updated_at: '2026-11-14' })] });
  assert.deepEqual(result.groups[0].conversations.map(item => item.id), ['latest', 'old', 'invalid']);
});
