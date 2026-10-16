export type QuestionAnswer = { option_id?: string; option_ids?: string[]; text?: string };
export type UserQuestion = { id: string; header?: string; prompt: string; multi_select?: boolean; options?: { id: string; label: string; description?: string; recommended?: boolean }[] };
export function validQuestionAnswer(question: UserQuestion, answer?: QuestionAnswer) {
 if (!answer || [...(answer.text || "")].length > 6000) return false;
 if (question.multi_select) {
  const selected = answer.option_ids || [];
  return !answer.option_id && selected.length === new Set(selected).size && selected.every(id => question.options?.some(option => option.id === id)) && Boolean(selected.length || answer.text?.trim());
 }
 return !answer.option_ids?.length && (!answer.option_id || Boolean(question.options?.some(option => option.id === answer.option_id))) && Boolean(answer.option_id || answer.text?.trim());
}
