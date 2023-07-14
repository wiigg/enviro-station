import { OpenAIClient, AzureKeyCredential } from "@azure/openai";

const endpoint = process.env["OpenAIEndpoint"];
const key = process.env["OpenAIKey"];

const client = new OpenAIClient(endpoint, new AzureKeyCredential(key));
const deployment = "es-ai-insights-gpt-35-turbo";
const maxTokens = 128;

const SYSTEM_MESSAGE =
  "You are an AI model with specialised knowledge in environmental science and a flair for humour. Your task is to analyze \
  the given particulate matter data and provide key insights. The data represents the average values for the past hour, \
  measured in micrograms per cubic meter of air.\
  Generate a short, scientifically accurate summary, adhering to the following guidelines: \
  1. Your analysis should align with the updated WHO guidelines: PM2.5 should be below 5, PM10 below 15, and PM1 should be kept as low as possible. \
  2. Include a practical tip on how individuals can enhance the air quality in their homes. Do not mention HVAC or HEPA filters. \
  3. Your response should be in the designated style given in the variable that starts with 'Style:'. \
  4. Avoid mentioning any specific data points directly from the provided data. \
  5. Do not mention the WHO guidelines in your response. \
  6. Your response should be no longer than four sentences. \
  Remember, this task calls for brevity and humour, coupled with scientific accuracy.";

const getCompletion = async (style, prompt) => {
  const messages = [
    {
      role: "system",
      content: SYSTEM_MESSAGE,
    },
    {
      role: "user",
      content: `Style: ${style} Data: ${prompt}`,
    },
  ];
  try {
    const response = await client.getChatCompletions(deployment, messages, {
      maxTokens,
      temperature: 0.7,
      n: 1,
      stream: false,
    });

    return response.choices[0].message["content"];
  } catch (e) {
    console.log(e);
    throw e; // Rethrow error or return a default value
  }
};

export default getCompletion;
