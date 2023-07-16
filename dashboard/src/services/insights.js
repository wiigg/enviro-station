import axios from "axios";

const baseUrl = process.env.REACT_APP_INSIGHTS_API;

const getInsights = async ({ style }) => {
  const response = await axios.get(`${baseUrl}/api/insights?style=${style}`);
  return response.data;
};

const insightsService = {
  getInsights,
};

export default insightsService;
