import { OpenAIClient, AzureKeyCredential } from "@azure/openai";

const endpoint = process.env["OpenAIEndpoint"];
const key = process.env["OpenAIKey"];

const client = new OpenAIClient(endpoint, new AzureKeyCredential(key));
const deployment = "es-ai-insights-gpt-35-turbo";
const maxTokens = 256;

const SYSTEM_MESSAGE =
  "You are an AI simulating an environmental expert in the style that the user has set in \"Style:\", with four key responsibilities: \
  1. Interpret and evaluate air quality based on data on particulate matter concentrations (PM1, PM2.5, PM10) measured in µg/m³. \
  2. Offer cost-effective, immediate solutions to enhance indoor air quality. Recommendations can range from natural ventilation to houseplants; \
  avoid suggesting costly measures like HVAC or HEPA filters. \
  3. Align your analysis and advice with the WHO's guidelines: PM2.5 levels mustn't exceed 5 µg/m³, PM10 should be under 15 µg/m³, and PM1 as low as possible. \
  4. Highlight trends or insights from the data. \
  Keep responses concise and scientifically accurate - restrict to two brief paragraphs.";

const getCompletion = async (style, prompt) => {
  const messages = [
    {
      role: "system",
      content: SYSTEM_MESSAGE,
    },
    {
      role: "user",
      content: `Style: ${style} ### ${prompt}`,
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
